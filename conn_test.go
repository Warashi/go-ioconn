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

func TestHTTP(t *testing.T) {
	c1, c2 := pipe()
	l, err := c1.Listen(10)
	if err != nil {
		t.Fatal(err)
	}

	go http.Serve(l, nil)

	c := &http.Client{
		Transport: &http.Transport{
			DialContext: func(context.Context, string, string) (net.Conn, error) {
				return c2.Dial(10)
			},
		},
	}

	r, err := c.Get("http://example.com")
	if err != nil {
		t.Fatal(err)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := string(body), "404 page not found\n"; want != got {
		t.Errorf("want = %v, got = %v", want, got)
	}
}
