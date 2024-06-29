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

type queue[T any] struct {
	mu      sync.Mutex
	buf     []T
	current int
	length  int
}

func (q *queue[T]) push(v T) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.buf = append(q.buf, v)
	q.length += 1
}

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

type Stream struct {
	conn *Conn

	closed atomic.Bool

	mu  sync.Mutex
	buf bytes.Buffer

	queue queue[*packet]

	readDeadline  atomic.Pointer[time.Time]
	writeDeadline atomic.Pointer[time.Time]

	localPort  uint64
	remotePort uint64
}

func (s *Stream) pushQueue(p *packet) {
	s.queue.push(p)
}

func (s *Stream) Read(b []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *Stream) Write(b []byte) (n int, err error) {
	if s.closed.Load() {
		return 0, net.ErrClosed
	}
	return s.conn.writeStream(s.remotePort, s.localPort, b)
}

func (s *Stream) Close() (err error) {
	if old := s.closed.Swap(true); old {
		return
	}
	if err := s.conn.sendFin(s.remotePort, s.localPort); err != nil {
		return err
	}
	return s.conn.closeStream(s.remotePort, s.localPort)
}

func (s *Stream) RemoteAddr() net.Addr { return Addr{s.remotePort} }
func (s *Stream) LocalAddr() net.Addr  { return Addr{s.localPort} }
func (s *Stream) SetDeadline(t time.Time) error {
	s.SetReadDeadline(t)
	s.SetWriteDeadline(t)
	return nil
}
func (s *Stream) SetReadDeadline(t time.Time) error {
	s.readDeadline.Store(&t)
	return nil
}
func (s *Stream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline.Store(&t)
	return nil
}
