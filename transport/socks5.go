package transport

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"

	"github.com/pkg/errors"
)

type socks5 struct {}

func NewSocks5() Transport {
	return &socks5{}
}

// Dial will dial a tcp connection with socks5 init
func (s *socks5) Dial(addr, targetAddr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, err
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	return &conn{
		init: make(chan struct{}),
		Conn: c,
		host: host,
		port: []byte{byte(portNum >> 8), byte(portNum)},
	}, nil
}

func (s *socks5) Listen(string) (<-chan *TargetConn, error) {
	panic("not support socks5 listener yet")
}

// socks5 init conn
type conn struct {
	init chan struct{}
	host string
	port []byte
	net.Conn
}

func (c *conn) Read(b []byte) (n int, err error) {
	<-c.init
	return c.Conn.Read(b)
}

func (c *conn) Write(b []byte) (n int, err error) {
	select {
	case <-c.init:
		return c.Conn.Write(b)
	default:
	}

	{
		req := &authReq{
			VER:      5,
			NMETHODS: 1,
			METHODS:  [1]byte{0}, // NO AUTHENTICATION REQUIRED
		}
		if err := binary.Write(c.Conn, binary.BigEndian, req); err != nil {
			return 0, err
		}
	}
	{
		resp := &authResp{}
		if err := binary.Read(c.Conn, binary.BigEndian, resp); err != nil {
			return 0, err
		}
	}
	{
		req := &request{
			req: req{
				VER:  5, // socks5
				CMD:  1, // CONNECT
				RSV:  0, // RESERVED
				ATYP: 3, // DOMAINNAME
			},
			DST_ADDR: append([]byte{byte(len(c.host))}, []byte(c.host)...),
			DST_PORT: c.port,
		}

		if _, err := c.Conn.Write(req.Bytes()); err != nil {
			return 0, err
		}
	}
	{
		resp := &response{}
		if err := binary.Read(c.Conn, binary.BigEndian, &(resp.resp)); err != nil {
			return 0, err
		}

		switch resp.REP {
		case 0x00:
		default:
			return 0, errors.Errorf("socks5 handshake fail, return code: %d", resp.REP)
		}

		switch resp.ATYP {
		case 0x01: // IPv4
			resp.DST_ADDR = make([]byte, net.IPv4len)
			if _, err := io.ReadFull(c.Conn, resp.DST_ADDR); err != nil {
				return 0, err
			}
		case 0x03: // domain name
			if _, err := io.ReadFull(c.Conn, resp.DST_ADDR[:1]); err != nil {
				return 0, err
			}
			if _, err := io.ReadFull(c.Conn, resp.DST_ADDR[1:1+int(resp.DST_ADDR[0])]); err != nil {
				return 0, err
			}
		case 0x04:
			resp.DST_ADDR = make([]byte, net.IPv6len)
			if _, err := io.ReadFull(c.Conn, resp.DST_ADDR); err != nil {
				return 0, err
			}
		}

		resp.DST_PORT = make([]byte, 2)
		if _, err := io.ReadFull(c.Conn, resp.DST_PORT); err != nil {
			return 0, err
		}
	}

	close(c.init)
	return c.Conn.Write(b)
}

type authReq struct {
	VER      byte
	NMETHODS byte
	METHODS  [1]byte // 1 to 255, fix to no authentication
}

type authResp struct {
	VER    byte
	METHOD byte
}

type request struct {
	req
	DST_ADDR []byte // first byte is length
	DST_PORT []byte // two bytes
}
type req struct {
	VER  byte
	CMD  byte
	RSV  byte
	ATYP byte
}

func (r *request) Bytes() []byte {
	out := []byte{r.VER, r.CMD, r.RSV, r.ATYP}
	out = append(out, r.DST_ADDR...)
	return append(out, r.DST_PORT...)
}

type response struct {
	resp
	DST_ADDR []byte // first byte is length
	DST_PORT []byte // two bytes
}
type resp struct {
	VER  byte
	REP  byte
	RSV  byte
	ATYP byte
}
