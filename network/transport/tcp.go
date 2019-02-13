package transport

import (
	"net"
	"strconv"
)

// TCP represents the TCP transport protocol alongside its respective configurable options.
type TCP struct {
	WriteBufferSize int
	ReadBufferSize  int
	NoDelay         bool
}

// NewTCP instantiates a new instance of the TCP transport protocol.
func NewTCP() *TCP {
	return &TCP{
		WriteBufferSize: 10000,
		ReadBufferSize:  10000,
		NoDelay:         false,
	}
}

// Listen listens for incoming TCP connections on a specified port.
func (t *TCP) Listen(port int) (interface{}, error) {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}

	return interface{}(listener), nil
}

// Dial dials an address via. the TCP protocol.
func (t *TCP) Dial(address string) (interface{}, error) {
	resolved, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, resolved)
	if err != nil {
		return nil, err
	}

	conn.SetWriteBuffer(t.WriteBufferSize)
	conn.SetReadBuffer(t.ReadBufferSize)
	conn.SetNoDelay(t.NoDelay)

	return interface{}(conn), nil
}
