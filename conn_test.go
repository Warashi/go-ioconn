package ioconn_test

import (
	"io"
	"testing"

	"github.com/Warashi/go-ioconn"
)

func init() {

}

func pipe() (*ioconn.Conn, *ioconn.Conn) {
  r1, w1 := io.Pipe()
  r2, w2 := io.Pipe()

	conn1 := ioconn.NewConn(w1, r2)
	conn2 := ioconn.NewConn(w2, r1)

	return conn1, conn2
}

func TestPackage(t *testing.T) {
	c1, c2 := pipe()
	l, err := c1.Listen(10)
	if err != nil {
		t.Fatal(err)
	}

  nc1, err := c2.Dial(10)
  if err != nil {
    t.Fatal(err)
  }

  nc2, err := l.Accept()
  if err != nil {
    t.Fatal(err)
  }

  if _, err := nc1.Write([]byte("test")); err != nil {
    t.Fatal(err)
  }
  if err := nc1.Close(); err != nil {
    t.Fatal(err)
  }
  
  b, err := io.ReadAll(nc2)
  if err != nil {
    t.Fatal(err)
  }

  if string(b) != "test" {
    t.Errorf("send `test` but received `%s`", string(b))
  }
}
