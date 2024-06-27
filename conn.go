package ioconn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"strconv"
	"sync"
)

var (
	StreamNotOpen = errors.New("stream is not open")
)

type packetType uint8

const (
	undefined packetType = iota
	ack
  synAck
	fin
  finAck
	data
)

var endian = binary.BigEndian

type Conn struct {
	rw io.ReadWriter

	mu sync.Mutex

	listener  map[uint64]*Listener
	usedPort  map[uint64]struct{}
	readBuf   map[port]*readBuffer
	writeInfo map[port]*writeInfo
}

type readWriter struct {
	io.Reader
	io.Writer
}

func NewConn(w io.Writer, r io.Reader) *Conn {
	c := &Conn{
		rw: readWriter{
			Reader: r,
			Writer: w,
		},

		listener: make(map[uint64]*Listener),
		usedPort: make(map[uint64]struct{}),
    readBuf: make(map[port]*readBuffer),
    writeInfo: make(map[port]*writeInfo),
	}

  go c.run()

	return c
}

func (c *Conn) run() {
  for {

  }
}

func (c *Conn) Listen(port uint64) (net.Listener, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.usedPort[port] = struct{}{}

	l := &Listener{
		port: port,
		ack:  make(chan *packet),
		done: make(chan struct{}),
	}

	c.listener[port] = l

	return l, nil
}

type Listener struct {
	conn *Conn

	port uint64
	ack  chan *packet
	done chan struct{}
}

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case ack := <-l.ack:
		return &Stream{
			conn:       l.conn,
			localPort:  l.port,
			remotePort: ack.sourcePort,
		}, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *Listener) Close() error {
	close(l.done)
	return nil
}

func (l *Listener) Addr() net.Addr {
	return Addr{port: l.port}
}

type Addr struct {
	port uint64
}

func (a Addr) Network() string {
	return "ioconn"
}

func (a Addr) String() string {
	return "ioconn:" + strconv.FormatUint(a.port, 10)
}

func (c *Conn) Dial(dest uint64) (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	local := uint64(math.MaxUint64)
	for i := range uint64(math.MaxUint64) {
		if _, ok := c.usedPort[i]; !ok {
			c.usedPort[i] = struct{}{}
			local = i
			break
		}
	}

	p := &packet{
		packetType:      ack,
		sourcePort:      local,
		destinationPort: dest,
	}

	if err := c.writePacket(p); err != nil {
		return nil, err
	}

	pkey := port{
		remote: dest,
		local:  local,
	}

	c.readBuf[pkey] = new(readBuffer)
	c.writeInfo[pkey] = new(writeInfo)

	return &Stream{
		conn:       c,
		localPort:  local,
		remotePort: dest,
	}, nil
}

type port struct {
	remote uint64
	local  uint64
}

type writeInfo struct {
	closed bool
	offset uint64
}

func (c *Conn) writePacket(p *packet) error {
	mb, err := p.MarshalBinary()
	if err != nil {
		return err
	}

	if _, err := c.rw.Write(mb); err != nil {
		return err
	}

	return nil
}

func (c *Conn) readStream(remote, local uint64, b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	buf, ok := c.readBuf[port{
		remote: remote,
		local:  local,
	}]
	if !ok {
		return 0, io.EOF
	}

	return buf.Read(b)
}

func (c *Conn) writeStream(remote, local uint64, b []byte) (n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.writeInfo[port{
		remote: remote,
		local:  local,
	}]
	if !ok {
		return 0, io.EOF
	}

	p := &packet{
		packetType:      data,
		sourcePort:      local,
		destinationPort: remote,
		offset:          info.offset,
		length:          uint16(len(b)),
		data:            b,
	}

	if err := c.writePacket(p); err != nil {
		return 0, err
	}

	info.offset += uint64(p.length)

	return int(p.length), nil
}

func (c *Conn) closeStream(remote, local uint64) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pkey := port{
		remote: remote,
		local:  local,
	}
	info, ok := c.writeInfo[pkey]
	if !ok {
		return nil
	}

	p := &packet{
		packetType:      fin,
		sourcePort:      local,
		destinationPort: remote,
	}

	if err := c.writePacket(p); err != nil {
		return err
	}

	info.closed = true

	return nil
}

type readBuffer struct {
	closed     bool
	buf        bytes.Buffer
	mu         sync.Mutex
	offset     uint64
	reorderBuf []*packet
}

func (b *readBuffer) Read(by []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, _ = b.buf.Read(by)
	if n <= len(by) {
		b.buf.Reset()
	}
	if b.closed {
		return n, io.EOF
	}
	return n, nil
}

func (b *readBuffer) Write(by []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(by)
}

func (b *readBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

type Stream struct {
	net.Conn
	conn *Conn

	localPort  uint64
	remotePort uint64
}

func (s *Stream) Read(b []byte) (n int, err error) {
	return s.conn.readStream(s.remotePort, s.localPort, b)
}

func (s *Stream) Write(b []byte) (n int, err error) {
	return s.conn.writeStream(s.remotePort, s.localPort, b)
}

func (s *Stream) Close() (err error) {
	return s.conn.closeStream(s.remotePort, s.localPort)
}

type packet struct {
	packetType      packetType
	sourcePort      uint64
	destinationPort uint64
	offset          uint64
	length          uint16
	data            []byte
}

func (p *packet) MarshalBinary() ([]byte, error) {
	if int(p.length) != len(p.data) {
		return nil, errors.New("data length is invalid")
	}

	data := make([]byte, 0, 27+p.length)

	data = append(data, byte(p.packetType))
	data = endian.AppendUint64(data, p.sourcePort)
	data = endian.AppendUint64(data, p.destinationPort)
	data = endian.AppendUint64(data, p.offset)
	data = endian.AppendUint16(data, p.length)
	data = append(data, data[:p.length]...)

	return data, nil
}

func (p *packet) UnmarshalBinary(data []byte) error {
	p.length = endian.Uint16(data[25:27])

	if int(19+p.length) != len(data) {
		return errors.New("data length is invalid")
	}

	p.packetType = packetType(data[0])
	p.sourcePort = endian.Uint64(data[1:9])
	p.destinationPort = endian.Uint64(data[9:17])
	p.offset = endian.Uint64(data[17:25])
	copy(p.data, data[27:])

	return nil
}
