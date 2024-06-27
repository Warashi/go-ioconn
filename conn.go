package ioconn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
)

var (
	StreamNotOpen = errors.New("stream is not open")
)

type packetType uint8

const (
	undefined packetType = 0
	data      packetType = 1
	fin       packetType = 2
	finAck    packetType = 3
)

var endian = binary.BigEndian

type Conn struct {
	rw io.ReadWriter

	mu sync.Mutex

	readBuf   map[port]*readBuffer
	writeInfo map[port]*writeInfo
}

type port struct {
	remote uint64
	local  uint64
}

type writeInfo struct {
	closed bool
	offset uint64
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

	mb, err := p.MarshalBinary()
	if err != nil {
		return 0, err
	}

	if _, err := c.rw.Write(mb); err != nil {
		return 0, err
	}

	info.offset += uint64(p.length)

	return int(p.length), nil
}

func (c *Conn) closeStream(remote, local uint64) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, ok := c.writeInfo[port{
		remote: remote,
		local:  local,
	}]
	if !ok {
		return nil
	}

	p := &packet{
		packetType:      fin,
		sourcePort:      local,
		destinationPort: remote,
	}

	mb, err := p.MarshalBinary()
	if err != nil {
		return err
	}

	if _, err := c.rw.Write(mb); err != nil {
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
	reorderBuf []*packet // not used when writing
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
	conn Conn

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
