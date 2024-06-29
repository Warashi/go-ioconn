package ioconn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"
)

var (
	StreamNotOpen = errors.New("stream is not open")
)

var endian = binary.BigEndian

type Conn struct {
	r         io.Reader
	readBufMu sync.Mutex
	readBuf   map[port]*readBuffer

	w           io.Writer
	writeInfoMu sync.Mutex
	writeInfo   map[port]*writeInfo

	stream map[port]*Stream

	listener map[uint64]*Listener
	usedPort map[uint64]struct{}
}

func NewConn(w io.Writer, r io.Reader) *Conn {
	c := &Conn{
		r: r,
		w: w,

		stream:    make(map[port]*Stream),
		listener:  make(map[uint64]*Listener),
		usedPort:  make(map[uint64]struct{}),
		readBuf:   make(map[port]*readBuffer),
		writeInfo: make(map[port]*writeInfo),
	}

	go c.run()

	return c
}

func (c *Conn) registerStream(s *Stream) {
	c.stream[port{remote: s.remotePort, local: s.localPort}] = s
}

func (c *Conn) next() bool {
	p, err := readPacket(c.r)
	if errors.Is(err, io.EOF) {
		return false
	}
	if err != nil {
		log.Printf("error while reading packet: %v\n", err)
		return false
	}
	log.Printf("packetType: %v", p.packetType)

	switch p.packetType {
	case ack:
		if listener, ok := c.listener[p.destinationPort]; ok {
			go func() {
				pkey := port{
					remote: p.sourcePort,
					local:  p.destinationPort,
				}

				c.readBuf[pkey] = new(readBuffer)
				c.writeInfo[pkey] = new(writeInfo)
				c.stream[pkey] = new(Stream)

				listener.ack <- p
			}()
		}
	case synAck:
	case fin:
		go func() {
			defer c.closeStream(p.sourcePort, p.destinationPort)
			if buf, ok := c.readBuf[port{remote: p.sourcePort, local: p.destinationPort}]; ok {
				buf.Close()
			}
			if err := c.writePacket(newPacket(
				finAck,
				p.destinationPort,
				p.sourcePort,
			)); err != nil {
				return
			}
		}()
	case finAck:
	case data:
		if s, ok := c.stream[port{remote: p.sourcePort, local: p.destinationPort}]; ok {
			s.pushQueue(p)
		}
	}

	return true
}

func (c *Conn) run() {
	for c.next() {
	}
}

func (c *Conn) Listen(port uint64) (net.Listener, error) {
	c.readBufMu.Lock()
	c.writeInfoMu.Lock()
	defer c.writeInfoMu.Unlock()
	defer c.readBufMu.Unlock()

	c.usedPort[port] = struct{}{}

	l := &Listener{
		conn: c,
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
	case p, ok := <-l.ack:
		if !ok {
			return nil, net.ErrClosed
		}
		if err := l.conn.writePacket(newPacket(
			synAck,
			p.destinationPort,
			p.sourcePort,
		)); err != nil {
			return nil, err
		}

		s := &Stream{
			conn:       l.conn,
			localPort:  l.port,
			remotePort: p.sourcePort,
		}
		l.conn.registerStream(s)

		return s, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *Listener) Close() error {
	delete(l.conn.listener, l.port)
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
	c.readBufMu.Lock()
	c.writeInfoMu.Lock()
	defer c.writeInfoMu.Unlock()
	defer c.readBufMu.Unlock()

	local := uint64(math.MaxUint64)
	for i := range uint64(math.MaxUint64) {
		if _, ok := c.usedPort[i]; !ok {
			c.usedPort[i] = struct{}{}
			local = i
			break
		}
	}

	if err := c.writePacket(newPacket(ack, local, dest)); err != nil {
		return nil, err
	}

	pkey := port{
		remote: dest,
		local:  local,
	}

	s := &Stream{
		conn:       c,
		localPort:  local,
		remotePort: dest,
	}
	c.registerStream(s)

	c.readBuf[pkey] = new(readBuffer)
	c.writeInfo[pkey] = new(writeInfo)

	return s, nil
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

	if _, err := c.w.Write(mb); err != nil {
		return err
	}

	return nil
}

func (c *Conn) writeStream(remote, local uint64, b []byte) (n int, err error) {
	c.writeInfoMu.Lock()
	defer c.writeInfoMu.Unlock()

	info, ok := c.writeInfo[port{
		remote: remote,
		local:  local,
	}]
	if !ok {
		return 0, io.EOF
	}

	p := newPacket(data, local, remote)
	p.offset = info.offset
	p.length = uint16(len(b))
	p.data = b

	if err := c.writePacket(p); err != nil {
		return 0, err
	}

	info.offset += uint64(p.length)

	return int(p.length), nil
}

func (c *Conn) sendFin(remote, local uint64) (err error) {
	if err := c.writePacket(newPacket(fin, local, remote)); err != nil {
		return err
	}
	return nil
}

func (c *Conn) closeStream(remote, local uint64) (err error) {
	c.writeInfoMu.Lock()
	defer c.writeInfoMu.Unlock()

	pkey := port{
		remote: remote,
		local:  local,
	}
	s, ok := c.stream[pkey]
	if ok {
		s.closed.Store(true)
	}

	info, ok := c.writeInfo[pkey]
	if ok {
		info.closed = true
	}

	return nil
}

type readBuffer struct {
	closed     bool
	buf        bytes.Buffer
	mu         sync.RWMutex
	nextOffset uint64
	reorderBuf []*packet
}

func (b *readBuffer) writePacket(p *packet) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if p.offset != b.nextOffset {
		b.reorderBuf = append(b.reorderBuf, p)
		return nil
	}

	if _, err := b.buf.Write(p.data); err != nil {
		return err
	}
	b.nextOffset = p.offset + uint64(p.length)

	slices.SortFunc(b.reorderBuf, func(a, b *packet) int {
		return int(a.offset) - int(b.offset)
	})

	for _, p := range b.reorderBuf {
		if p.offset != b.nextOffset {
			return nil
		}

		if _, err := b.buf.Write(p.data); err != nil {
			return err
		}
		b.nextOffset = p.offset + uint64(p.length)
	}

	return nil
}

func (b *readBuffer) Read(by []byte) (n int, err error) {
	for {
		n, err = b.buf.Read(by)
		if n == 0 && errors.Is(err, io.EOF) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		break
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return n, io.EOF
	}
	return n, nil
}

func (b *readBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}
