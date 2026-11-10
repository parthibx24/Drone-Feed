package gateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"

	"github.com/parthibx24/Drone-Feed/internal/mav"
)

const (
	paramIdle         = 2 * time.Second
	paramReadTimeout  = 1500 * time.Millisecond
	paramReadAttempts = 3
	paramFetchMax     = 90 * time.Second
)

// Param is one autopilot parameter.
type Param struct {
	Name  string  `json:"name"`
	Value float32 `json:"value"`
	Type  uint8   `json:"type"`
	Index uint16  `json:"index"`
}

func (d *Drone) paramFilter(sys uint8, name string) func(*gomavlib.EventFrame) bool {
	return func(fr *gomavlib.EventFrame) bool {
		pv, ok := fr.Message().(*common.MessageParamValue)
		return ok && fr.SystemID() == sys && fr.ComponentID() == mav.CompAutopilot && (name == "" || pv.ParamId == name)
	}
}

// CachedParams returns parameters seen so far without contacting the vehicle.
func (d *Drone) CachedParams() []Param {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Param, 0, len(d.params))
	for _, p := range d.params {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FetchParams downloads the full parameter list, re-requesting any indexes
// lost on the link.
func (d *Drone) FetchParams(ctx context.Context) ([]Param, error) {
	sys, err := d.targetSystem()
	if err != nil {
		return nil, err
	}
	if err := d.lockParams(ctx); err != nil {
		return nil, err
	}
	defer d.unlockParams()
	ctx, cancel := context.WithTimeout(ctx, paramFetchMax)
	defer cancel()

	ch, unsub := d.subscribe(d.paramFilter(sys, ""))
	defer unsub()

	got := map[uint16]bool{}
	total := -1
	collect := func() error {
		idle := time.NewTimer(paramIdle)
		defer idle.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-idle.C:
				return nil
			case fr := <-ch:
				pv := fr.Message().(*common.MessageParamValue)
				if pv.ParamIndex != 65535 {
					got[pv.ParamIndex] = true
					total = int(pv.ParamCount)
				}
				if total >= 0 && len(got) >= total {
					return nil
				}
				idle.Reset(paramIdle)
			}
		}
	}

	if err := d.write(&common.MessageParamRequestList{TargetSystem: sys, TargetComponent: mav.CompAutopilot}); err != nil {
		return nil, err
	}
	if err := collect(); err != nil {
		return nil, err
	}
	if total < 0 {
		return nil, fmt.Errorf("parameter list: %w", ErrTimeout)
	}
	for pass := 0; pass < 3 && len(got) < total; pass++ {
		for i := 0; i < total; i++ {
			if !got[uint16(i)] {
				_ = d.write(&common.MessageParamRequestRead{TargetSystem: sys, TargetComponent: mav.CompAutopilot, ParamIndex: int16(i)})
			}
		}
		if err := collect(); err != nil {
			return nil, err
		}
	}
	if len(got) < total {
		d.log.Warn("parameter list incomplete", "got", len(got), "total", total)
	}
	return d.CachedParams(), nil
}

// GetParam reads one parameter by name.
func (d *Drone) GetParam(ctx context.Context, name string) (Param, error) {
	sys, err := d.targetSystem()
	if err != nil {
		return Param{}, err
	}
	if len(name) > 16 {
		return Param{}, fmt.Errorf("parameter names are at most 16 characters")
	}
	ch, unsub := d.subscribe(d.paramFilter(sys, name))
	defer unsub()
	for attempt := 0; attempt < paramReadAttempts; attempt++ {
		if err := d.write(&common.MessageParamRequestRead{
			TargetSystem: sys, TargetComponent: mav.CompAutopilot, ParamId: name, ParamIndex: -1,
		}); err != nil {
			return Param{}, err
		}
		select {
		case <-ctx.Done():
			return Param{}, ctx.Err()
		case <-time.After(paramReadTimeout):
		case fr := <-ch:
			pv := fr.Message().(*common.MessageParamValue)
			return Param{Name: pv.ParamId, Value: pv.ParamValue, Type: uint8(pv.ParamType), Index: pv.ParamIndex}, nil
		}
	}
	return Param{}, fmt.Errorf("parameter %s: %w (does it exist?)", name, ErrTimeout)
}

// SetParam writes a parameter and confirms the value the vehicle echoes back.
func (d *Drone) SetParam(ctx context.Context, name string, value float32) (Param, error) {
	cur, err := d.GetParam(ctx, name) // confirms it exists and gives us its type
	if err != nil {
		return Param{}, err
	}
	sys, err := d.targetSystem()
	if err != nil {
		return Param{}, err
	}
	if err := d.lockParams(ctx); err != nil {
		return Param{}, err
	}
	defer d.unlockParams()
	ch, unsub := d.subscribe(d.paramFilter(sys, name))
	defer unsub()
	matches := func(v float32) bool {
		return math.Abs(float64(v-value)) <= 1e-4*math.Max(1, math.Abs(float64(value)))
	}
	// A PARAM_VALUE with a different value may be a late reply to an earlier
	// read (or another GCS's download), not the answer to this write. Keep
	// listening for the echo for the whole window; only a different value with
	// no matching echo means the vehicle rejected it.
	var other *Param
	for attempt := 0; attempt < paramReadAttempts; attempt++ {
		// Never send once the caller has given up: it would report failure for
		// a change that then happens.
		if err := ctx.Err(); err != nil {
			return Param{}, err
		}
		if err := d.write(&common.MessageParamSet{
			TargetSystem: sys, TargetComponent: mav.CompAutopilot, ParamId: name,
			ParamValue: value, ParamType: common.MAV_PARAM_TYPE(cur.Type),
		}); err != nil {
			return Param{}, err
		}
		window := time.After(paramReadTimeout)
	wait:
		for {
			select {
			case <-ctx.Done():
				return Param{}, fmt.Errorf("parameter %s: %w (the write may still have been applied)", name, ctx.Err())
			case <-window:
				break wait
			case fr := <-ch:
				pv := fr.Message().(*common.MessageParamValue)
				p := Param{Name: pv.ParamId, Value: pv.ParamValue, Type: uint8(pv.ParamType), Index: pv.ParamIndex}
				if matches(pv.ParamValue) {
					return p, nil
				}
				other = &p
			}
		}
		if other != nil {
			return *other, fmt.Errorf("parameter %s: %w: vehicle kept %v (out of range or read-only)", name, ErrRejected, other.Value)
		}
	}
	return Param{}, fmt.Errorf("parameter %s: %w", name, ErrTimeout)
}

// lockParams takes the parameter-transaction slot, giving up when ctx ends so
// a long download can't make a write hang past its deadline.
func (d *Drone) lockParams(ctx context.Context) error {
	select {
	case d.paramSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("parameters busy (a download is in progress): %w", ctx.Err())
	}
}

func (d *Drone) unlockParams() { <-d.paramSem }
