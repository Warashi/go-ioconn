package ioconn

import (
	"bytes"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var _ net.Conn = (*Stream)(nil)

// queue is the generic queue implementation
type queue[T any] struct {
	mu      sync.Mutex
	buf     []T
	current int
	length  int
}

// push an item to the queue
func (q *queue[T]) push(v T) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.buf = append(q.buf, v)
	q.length += 1
}

// pop an item from the queue and resize the backed array if needed
func (q *queue[T]) pop() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.length <= q.current {
		var zero T
		return zero, false
	}
	ret := q.buf[q.current]
	q.current++

	if (q.length / 2) < q.current {
		l := q.length - q.current
		copy(q.buf[:l], q.buf[q.current:q.length])
		q.current = 0
		q.length = l
	}

	return ret, true
}

// Stream implements net.Conn
type Stream struct {
	conn *Conn

	closed atomic.Bool

	written uint64
	writeMu sync.Mutex

	readMu sync.Mutex
	buf    bytes.Buffer

	queue queue[*packet]

	readDeadline  atomic.Pointer[time.Time]
	writeDeadline atomic.Pointer[time.Time]

	localPort  uint64
	remotePort uint64
}

// pushQueue pushes the packet to its underlaying queue
func (s *Stream) pushQueue(p *packet) {
	s.queue.push(p)
}

// Read implements io.Reader
func (s *Stream) Read(b []byte) (n int, err error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()

	defer func() {
		if s.buf.Len() == 0 {
			s.buf.Reset()
		}
	}()

	for {
		if p, ok := s.queue.pop(); ok {
			if _, err := s.buf.Write(p.data); err != nil {
				return 0, err
			}
			return s.buf.Read(b)
		}
		if d := s.readDeadline.Load(); d != nil && d.After(time.Now()) {
			return 0, os.ErrDeadlineExceeded
		}
		if s.closed.Load() {
			return 0, io.EOF
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Write implements io.Writer
func (s *Stream) Write(b []byte) (n int, err error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.closed.Load() {
		return 0, net.ErrClosed
	}
	if err := s.conn.writePacket(newDataPacket(s.localPort, s.remotePort, s.written, b)); err != nil {
		return 0, err
	}
	s.written += uint64(len(b))

	return len(b), nil
}

// Close implements io.Closer
func (s *Stream) Close() (err error) {
	if old := s.closed.Swap(true); old {
		return
	}
	if err := s.conn.sendFin(s.remotePort, s.localPort); err != nil {
		return err
	}
	return s.conn.closeStream(s.remotePort, s.localPort)
}

// RemoteAddr implements net.Conn
func (s *Stream) RemoteAddr() net.Addr { return Addr{s.remotePort} }
// LocalAddr implements net.Conn
func (s *Stream) LocalAddr() net.Addr  { return Addr{s.localPort} }
// SetDeadline implements net.Conn
func (s *Stream) SetDeadline(t time.Time) error {
	s.SetReadDeadline(t)
	s.SetWriteDeadline(t)
	return nil
}
// SetReadDeadline implements net.Conn
func (s *Stream) SetReadDeadline(t time.Time) error {
	s.readDeadline.Store(&t)
	return nil
}
// SetWriteDeadline implements net.Conn
func (s *Stream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline.Store(&t)
	return nil
}
