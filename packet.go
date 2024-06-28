package ioconn

import (
	"errors"
	"io"
)

type packet struct {
	packetType      packetType
	sourcePort      uint64
	destinationPort uint64
	offset          uint64
	length          uint16
	data            []byte
}

func readPacket(r io.Reader) (*packet, error) {
	panic("unimplemented")
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
