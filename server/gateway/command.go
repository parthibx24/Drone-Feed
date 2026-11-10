package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bluenviron/gomavlib/v3"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/message"
)

const (
	ackTimeout        = 1500 * time.Millisecond
	inProgressTimeout = 10 * time.Second
	commandAttempts   = 3
)

// CommandError is returned when the vehicle answers with anything but ACCEPTED.
type CommandError struct {
	Command common.MAV_CMD
	Result  common.MAV_RESULT
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("%s: %s", shortCmd(e.Command), shortResult(e.Result))
}

func shortCmd(c common.MAV_CMD) string       { return strings.TrimPrefix(c.String(), "MAV_CMD_") }
func shortResult(r common.MAV_RESULT) string { return strings.TrimPrefix(r.String(), "MAV_RESULT_") }

// CommandLong sends COMMAND_LONG to component comp and waits for COMMAND_ACK,
// retrying with an incremented confirmation counter. Returns *CommandError if
// the vehicle rejects it, ErrTimeout if it never answers.
func (d *Drone) CommandLong(ctx context.Context, comp uint8, cmd common.MAV_CMD, p [7]float32) (common.MAV_RESULT, error) {
	return d.command(ctx, comp, cmd, func(sys uint8, attempt int) message.Message {
		return &common.MessageCommandLong{
			TargetSystem: sys, TargetComponent: comp, Command: cmd, Confirmation: uint8(attempt),
			Param1: p[0], Param2: p[1], Param3: p[2], Param4: p[3], Param5: p[4], Param6: p[5], Param7: p[6],
		}
	})
}

// CommandInt sends COMMAND_INT (positions as degE7 integers) and waits for the ACK.
func (d *Drone) CommandInt(ctx context.Context, comp uint8, cmd common.MAV_CMD, fr common.MAV_FRAME,
	p [4]float32, x, y int32, z float32) (common.MAV_RESULT, error) {
	return d.command(ctx, comp, cmd, func(sys uint8, _ int) message.Message {
		return &common.MessageCommandInt{
			TargetSystem: sys, TargetComponent: comp, Frame: fr, Command: cmd,
			Param1: p[0], Param2: p[1], Param3: p[2], Param4: p[3], X: x, Y: y, Z: z,
		}
	})
}

func (d *Drone) command(ctx context.Context, comp uint8, cmd common.MAV_CMD,
	build func(sys uint8, attempt int) message.Message) (common.MAV_RESULT, error) {
	sys, err := d.targetSystem()
	if err != nil {
		return 0, err
	}

	// One command at a time per drone keeps ACK matching unambiguous.
	d.cmdMu.Lock()
	defer d.cmdMu.Unlock()

	acks, cancel := d.subscribe(func(fr *gomavlib.EventFrame) bool {
		ack, ok := fr.Message().(*common.MessageCommandAck)
		return ok && ack.Command == cmd && fr.SystemID() == sys &&
			(comp == 0 || fr.ComponentID() == comp) &&
			d.addressedToUs(ack.TargetSystem, ack.TargetComponent)
	})
	defer cancel()

	for attempt := 0; attempt < commandAttempts; attempt++ {
		if err := d.write(build(sys, attempt)); err != nil {
			return 0, err
		}
		timeout := time.NewTimer(ackTimeout)
	wait:
		for {
			select {
			case <-ctx.Done():
				timeout.Stop()
				return 0, ctx.Err()
			case <-timeout.C:
				break wait // resend
			case fr := <-acks:
				ack := fr.Message().(*common.MessageCommandAck)
				if ack.Result == common.MAV_RESULT_IN_PROGRESS {
					timeout.Reset(inProgressTimeout)
					continue
				}
				timeout.Stop()
				if ack.Result != common.MAV_RESULT_ACCEPTED {
					return ack.Result, &CommandError{Command: cmd, Result: ack.Result}
				}
				return ack.Result, nil
			}
		}
	}
	return 0, fmt.Errorf("%s: %w", shortCmd(cmd), ErrTimeout)
}
