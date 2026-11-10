package skydroid

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client sends commands to the gimbal over UDP. It is safe for concurrent use.
//
// Line endings follow the original implementation: motion, PTZ and step zoom
// are sent bare; photo, record, absolute zoom, palette and queries get CRLF.
type Client struct {
	Encoder

	mu     sync.Mutex
	conn   *net.UDPConn
	remote *net.UDPAddr
}

// Dial binds localPort and targets host:port.
func Dial(host string, port, localPort int, enc SpeedEncoding) (*Client, error) {
	remote, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: localPort})
	if err != nil {
		return nil, fmt.Errorf("bind UDP %d: %w (is the old drone-agent or the Skydroid app still running?)", localPort, err)
	}
	return &Client{Encoder: Encoder{Speed: enc}, conn: conn, remote: remote}, nil
}

// Close releases the socket.
func (c *Client) Close() error { return c.conn.Close() }

// Send writes one command without CRLF.
func (c *Client) Send(cmd string) error { return c.write(cmd, false) }

// SendCRLF writes one command followed by CRLF.
func (c *Client) SendCRLF(cmd string) error { return c.write(cmd, true) }

func (c *Client) write(cmd string, crlf bool) error {
	b := []byte(cmd)
	if crlf {
		b = append(b, '\r', '\n')
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.WriteToUDP(b, c.remote)
	return err
}

// Query sends cmd with CRLF and waits up to timeout for one reply datagram.
func (c *Client) Query(cmd string, timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.conn.WriteToUDP(append([]byte(cmd), '\r', '\n'), c.remote); err != nil {
		return "", err
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	defer c.conn.SetReadDeadline(time.Time{}) //nolint:errcheck
	buf := make([]byte, 4096)
	for {
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return "", errors.New("no reply from camera")
			}
			return "", err
		}
		if from.IP.Equal(c.remote.IP) {
			return string(buf[:n]), nil
		}
	}
}
