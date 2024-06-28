package ioconn

import "net"

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
