package ioconn

import (
	"errors"
	"io"
	"log"
)

//go:generate go run golang.org/x/tools/cmd/stringer@latest -type packetType
type packetType uint8

const (
	undefined packetType = iota
	ack
	synAck
	fin
	finAck
	data
)

type packet struct {
	packetType      packetType
	sourcePort      uint64
	destinationPort uint64
	offset          uint64
	length          uint16
	data            []byte
}

func newPacket(typ packetType, local, remote uint64) *packet {
	return &packet{
		packetType:      typ,
		sourcePort:      local,
		destinationPort: remote,
	}
}

func newDataPacket(local, remote, offset uint64, b []byte) *packet {
	p := newPacket(data, local, remote)
	p.offset = offset
	p.length = uint16(len(b))
	p.data = b

	return p
}

func readPacket(r io.Reader) (*packet, error) {
	var header [17]byte
	if _, err := r.Read(header[:]); err != nil {
		return nil, err
	}

	log.Println(header)

	p := &packet{}
	p.packetType = packetType(header[0])
	p.sourcePort = endian.Uint64(header[1:9])
	p.destinationPort = endian.Uint64(header[9:17])

	if p.packetType != data {
		return p, nil
	}

	var dataHeader [10]byte
	if _, err := r.Read(dataHeader[:]); err != nil {
		return nil, err
	}

	p.offset = endian.Uint64(dataHeader[:8])
	p.length = endian.Uint16(dataHeader[8:])

	p.data = make([]byte, p.length)
	if _, err := r.Read(p.data); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *packet) MarshalBinary() ([]byte, error) {
	if int(p.length) != len(p.data) {
		return nil, errors.New("data length is invalid")
	}

	b := make([]byte, 0, 27+p.length)

	b = append(b, byte(p.packetType))
	b = endian.AppendUint64(b, p.sourcePort)
	b = endian.AppendUint64(b, p.destinationPort)
	if p.packetType == data {
		b = endian.AppendUint64(b, p.offset)
		b = endian.AppendUint16(b, p.length)
		b = append(b, p.data...)
	}

	return b, nil
}

func (p *packet) UnmarshalBinary(b []byte) error {
	p.length = endian.Uint16(b[25:27])

	if int(19+p.length) != len(b) {
		return errors.New("data length is invalid")
	}

	p.packetType = packetType(b[0])
	p.sourcePort = endian.Uint64(b[1:9])
	p.destinationPort = endian.Uint64(b[9:17])

	if p.packetType == data {
		p.offset = endian.Uint64(b[17:25])
		p.length = endian.Uint16(b[25:27])
		copy(p.data, b[27:])
	}

	return nil
}
