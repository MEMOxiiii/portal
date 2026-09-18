package socket

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/socket/packet"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"go.uber.org/atomic"
	"io"
	"net"
	"sync"
	"time"
)

// writeTimeout bounds every WritePacket call, so a peer that stops reading can't block a write forever --
// unlike the read side, which is left undeadlined post-auth so an idle backend isn't killed for going quiet.
// A var, not a const, so tests can shrink it.
var writeTimeout = 10 * time.Second

// Client represents a client connected over the TCP socket system.
type Client struct {
	log  internal.Logger
	conn net.Conn

	readerLimits bool

	pool packet.Pool

	sendMu sync.Mutex
	hdr    *packet.Header
	buf    *bytes.Buffer

	name          string
	authenticated atomic.Bool
}

// NewClient creates a new socket Client with default allocations and required data. It pre-allocates 4096
// bytes to prevent allocations during runtime as much as possible.
func NewClient(conn net.Conn, log internal.Logger, readerLimits bool) *Client {
	return &Client{
		log:  log,
		conn: conn,

		readerLimits: readerLimits,

		pool: packet.NewPool(),
		buf:  bytes.NewBuffer(make([]byte, 0, 4096)),
		hdr:  &packet.Header{},
	}
}

// Name returns the name the client authenticated with.
func (c *Client) Name() string {
	return c.name
}

// Close closes the client and related connections.
func (c *Client) Close() error {
	return c.conn.Close()
}

// Authenticate marks the client as authenticated and gives it the provided name.
func (c *Client) Authenticate(name string) {
	if c.authenticated.CAS(false, true) {
		c.name = name
	}
}

// Authenticated returns if the client has been authenticated or not.
func (c *Client) Authenticated() bool {
	return c.authenticated.Load()
}

// maxPacketSize bounds the allocation ReadPacket makes from an unauthenticated peer's length prefix.
const maxPacketSize = 1 << 20 // 1 MiB

// ReadPacket reads a packet from the connection and returns it. The client is expected to prefix the packet
// payload with 4 bytes for the length of the payload.
func (c *Client) ReadPacket() (pk packet.Packet, err error) {
	var l uint32
	if err := binary.Read(c.conn, binary.LittleEndian, &l); err != nil {
		return nil, err
	}
	if l > maxPacketSize {
		return nil, fmt.Errorf("packet size %v exceeds maximum of %v", l, maxPacketSize)
	}

	data := make([]byte, l)
	if _, err := io.ReadFull(c.conn, data); err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(data)
	header := &packet.Header{}
	if err := header.Read(buf); err != nil {
		return nil, err
	}

	pk, ok := c.pool[header.PacketID]
	if !ok {
		return nil, fmt.Errorf("unknown packet %v", header.PacketID)
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%T: %v", pk, r)
		}
	}()
	pk.Unmarshal(protocol.NewReader(buf, 0, c.readerLimits))
	if buf.Len() > 0 {
		return nil, fmt.Errorf("still have %v bytes unread", buf.Len())
	}

	return pk, nil
}

// WritePacket writes a packet to the client. Since it's a TCP connection, the payload is prefixed with a
// length so the client can read the exact length of the packet.
func (c *Client) WritePacket(pk packet.Packet) (err error) {
	// sendMu covers the whole send, not just the marshal: releasing it right after Bytes() let a
	// concurrent WritePacket (e.g. the latency reporter racing a request handler on the same Client)
	// overwrite c.buf's backing array while this call was still writing it to the connection.
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	defer c.buf.Reset()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%T: %v", pk, r)
		}
	}()

	c.hdr.PacketID = pk.ID()
	_ = c.hdr.Write(c.buf)
	pk.Marshal(protocol.NewWriter(c.buf, 0))

	framed := make([]byte, 4+c.buf.Len())
	binary.LittleEndian.PutUint32(framed, uint32(c.buf.Len()))
	copy(framed[4:], c.buf.Bytes())

	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = c.conn.Write(framed)
	return err
}
