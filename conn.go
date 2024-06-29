package ioconn

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

var (
	StreamNotOpen = errors.New("stream is not open")
)

var endian = binary.BigEndian

type Conn struct {
	r  io.Reader
	w  io.Writer
	mu sync.Mutex

	closed   atomic.Bool
	stream   map[port]*Stream
	listener map[uint64]*Listener
	usedPort map[uint64]struct{}
}

func NewConn(w io.Writer, r io.Reader) *Conn {
	c := &Conn{
		r: r,
		w: w,

		stream:   make(map[port]*Stream),
		listener: make(map[uint64]*Listener),
		usedPort: make(map[uint64]struct{}),
	}

	go c.run()

	return c
}

func (c *Conn) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *Conn) registerStream(s *Stream) {
	c.mu.Lock()
	defer c.mu.Unlock()

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
				listener.ack <- p
			}()
		}
	case synAck:
	case fin:
		go func() {
			c.closeStream(p.sourcePort, p.destinationPort)
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
	for !c.closed.Load() && c.next() {
	}
}

func (c *Conn) Listen(port uint64) (net.Listener, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	if err := c.writePacket(newPacket(ack, local, dest)); err != nil {
		return nil, err
	}

	s := &Stream{
		conn:       c,
		localPort:  local,
		remotePort: dest,
	}

	// we cannot use registerStream because deadlock occurs
	c.stream[port{remote: s.remotePort, local: s.localPort}] = s

	return s, nil
}

type port struct {
	remote uint64
	local  uint64
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

func (c *Conn) sendFin(remote, local uint64) (err error) {
	if err := c.writePacket(newPacket(fin, local, remote)); err != nil {
		return err
	}
	return nil
}

func (c *Conn) closeStream(remote, local uint64) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pkey := port{
		remote: remote,
		local:  local,
	}
	s, ok := c.stream[pkey]
	if ok {
		s.closed.Store(true)
	}

	return nil
}
