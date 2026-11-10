package mav

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bluenviron/gomavlib/v3"
)

// ParseEndpoint turns a short endpoint string into a gomavlib endpoint:
//
//	udp:HOST:PORT          UDP client  (we send first; peer replies to us)
//	udp-server:ADDR:PORT   UDP server  (peer sends first; we reply to it)
//	tcp:HOST:PORT          TCP client  (reconnects automatically)
//	tcp-server:ADDR:PORT   TCP server
//	serial:DEVICE:BAUD     serial port, e.g. serial:/dev/ttyAMA0:115200
func ParseEndpoint(s string) (gomavlib.EndpointConf, error) {
	kind, rest, ok := strings.Cut(s, ":")
	if !ok || rest == "" {
		return nil, fmt.Errorf("endpoint %q: want kind:address", s)
	}
	switch kind {
	case "udp":
		return gomavlib.EndpointUDPClient{Address: rest}, nil
	case "udp-server":
		return gomavlib.EndpointUDPServer{Address: rest}, nil
	case "tcp":
		return gomavlib.EndpointTCPClient{Address: rest}, nil
	case "tcp-server":
		return gomavlib.EndpointTCPServer{Address: rest}, nil
	case "serial":
		i := strings.LastIndex(rest, ":")
		if i < 0 {
			return nil, fmt.Errorf("endpoint %q: want serial:DEVICE:BAUD", s)
		}
		baud, err := strconv.Atoi(rest[i+1:])
		if err != nil {
			return nil, fmt.Errorf("endpoint %q: bad baud: %w", s, err)
		}
		return gomavlib.EndpointSerial{Device: rest[:i], Baud: baud}, nil
	}
	return nil, fmt.Errorf("endpoint %q: unknown kind %q (udp, udp-server, tcp, tcp-server, serial)", s, kind)
}
