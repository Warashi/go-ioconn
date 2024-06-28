package ioconn_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/Warashi/go-ioconn"
)

func pipe() (*ioconn.Conn, *ioconn.Conn) {
  r1, w1 := io.Pipe()
  r2, w2 := io.Pipe()

  conn1 := ioconn.NewConn(w1, r2)
  conn2 := ioconn.NewConn(w2, r1)

  return conn1, conn2
}

func TestPackage(t *testing.T) {
  c1, c2 := pipe()
  mux := http.NewServeMux()
  l, err := c1.Listen(10)
  if err != nil {
    t.Fatal(err)
  }
  go http.Serve(l, mux)

  c := &http.Client{
    Transport: &http.Transport{
      DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
        return c2.Dial(10)
      },
    },
  }

  resp, err := c.Get("hoge")
  if err != nil {
    t.Fatal(err)
  }
  io.Copy(io.Discard, resp.Body)
}
