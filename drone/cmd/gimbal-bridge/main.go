// gimbal-bridge runs on the drone's Raspberry Pi. It makes the Skydroid C12
// appear on the MAVLink network as a standard camera (component 100) and
// gimbal (component 154), translating MAVLink commands to Skydroid UDP.
//
// Every flag can also be set with an environment variable: -gimbal-host is
// GIMBAL_HOST, -mavlink is MAVLINK, and so on.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/parthibx24/Drone-Feed/drone/bridge"
	"github.com/parthibx24/Drone-Feed/drone/skydroid"
	"github.com/parthibx24/Drone-Feed/internal/mav"
)

var version = "dev"

func main() {
	fs := flag.NewFlagSet("gimbal-bridge", flag.ExitOnError)
	var (
		endpoint     = fs.String("mavlink", "udp-server:127.0.0.1:14556", "MAVLink endpoint (see mav.ParseEndpoint)")
		sysID        = fs.Uint("sysid", 0, "vehicle system ID (0 = learn from autopilot heartbeat)")
		gimbalHost   = fs.String("gimbal-host", "192.168.144.108", "C12 IP on the onboard LAN")
		gimbalPort   = fs.Int("gimbal-port", 5000, "C12 control UDP port")
		localPort    = fs.Int("local-port", 5000, "local UDP port to bind for the C12")
		speedEnc     = fs.String("speed-encoding", "legacy", "speed encoding: legacy or apk")
		motionHz     = fs.Float64("motion-rate-hz", 10, "speed packet repeat rate while moving")
		rateTimeout  = fs.Duration("rate-timeout", 600*time.Millisecond, "stop rate motion if not refreshed within this time")
		maxRate      = fs.Float64("max-rate", 30, "deg/s for full-scale manual control input")
		videoVisible = fs.String("video-visible", "", "RTSP URI advertised for the visible stream (empty = none)")
		videoThermal = fs.String("video-thermal", "", "RTSP URI advertised for the thermal stream (empty = none)")
		logLevel     = fs.String("log-level", "info", "debug, info, warn, error")
		showVersion  = fs.Bool("version", false, "print version and exit")
	)
	envDefaults(fs)
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println(version)
		return
	}

	log := newLogger(*logLevel)
	if err := run(log, *endpoint, uint8(*sysID), *gimbalHost, *gimbalPort, *localPort, *speedEnc,
		*motionHz, *rateTimeout, *maxRate, *videoVisible, *videoThermal); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, endpoint string, sysID uint8, host string, port, localPort int, encName string,
	motionHz float64, rateTimeout time.Duration, maxRate float64, visible, thermal string) error {
	ep, err := mav.ParseEndpoint(endpoint)
	if err != nil {
		return err
	}
	enc, err := skydroid.ParseSpeedEncoding(encName)
	if err != nil {
		return err
	}
	g, err := skydroid.Dial(host, port, localPort, enc)
	if err != nil {
		return err
	}
	defer g.Close()

	var streams []bridge.Stream
	if visible != "" {
		streams = append(streams, bridge.Stream{Name: "visible", URI: visible})
	}
	if thermal != "" {
		streams = append(streams, bridge.Stream{Name: "thermal", URI: thermal, Thermal: true})
	}

	b, err := bridge.New(bridge.Config{
		Endpoint: ep, SystemID: sysID, Gimbal: g, MotionRateHz: motionHz,
		RateTimeout: rateTimeout, MaxRate: maxRate, Streams: streams, Log: log,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("gimbal-bridge started", "version", version, "mavlink", endpoint,
		"gimbal", fmt.Sprintf("%s:%d", host, port), "local_port", localPort, "encoding", enc)
	return b.Run(ctx)
}

// envDefaults lets FOO_BAR override -foo-bar.
func envDefaults(fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		key := strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if v, ok := os.LookupEnv(key); ok {
			_ = f.Value.Set(v)
		}
	})
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	_ = l.UnmarshalText([]byte(level))
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}
