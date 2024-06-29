package ioconn

import (
	"net"
	"time"
)

var _ net.Conn = (*Stream)(nil)

type Stream struct {
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
  if err := s.conn.sendFin(s.remotePort, s.localPort); err != nil {
    return err
  }
	return s.conn.closeStream(s.remotePort, s.localPort)
}

func (s *Stream) RemoteAddr() net.Addr               { return Addr{s.remotePort} }
func (s *Stream) LocalAddr() net.Addr                { return Addr{s.localPort} }
func (s *Stream) SetDeadline(t time.Time) error      { return nil }
func (s *Stream) SetReadDeadline(t time.Time) error  { return nil }
func (s *Stream) SetWriteDeadline(t time.Time) error { return nil }
