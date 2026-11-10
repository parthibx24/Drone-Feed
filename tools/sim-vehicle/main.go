// sim-vehicle is a fake ArduCopter for developing against drone-gateway
// without hardware. Point a gateway drone at it with
//
//	mavlink: udp:127.0.0.1:14560
//
// It is not a flight simulator; use ArduPilot SITL for real flight behaviour.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bluenviron/gomavlib/v3"

	"github.com/parthibx24/Drone-Feed/internal/sim"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:14560", "UDP address the gateway connects to")
	bridge := flag.String("bridge", "", "optional gimbal-bridge UDP address to route to (e.g. 127.0.0.1:14556)")
	prearmFail := flag.Bool("prearm-fail", false, "refuse to arm (to test error handling)")
	flag.Parse()

	eps := []gomavlib.EndpointConf{gomavlib.EndpointUDPServer{Address: *listen}}
	if *bridge != "" {
		eps = append(eps, gomavlib.EndpointUDPClient{Address: *bridge})
	}
	v, err := sim.New(eps, *bridge != "")
	if err != nil {
		log.Fatal(err)
	}
	defer v.Close()
	v.Set(func(v *sim.Vehicle) { v.PrearmFail = *prearmFail })

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("sim-vehicle: sysid 1 on udp %s (bridge: %q)", *listen, *bridge)
	v.Run(ctx)
}
