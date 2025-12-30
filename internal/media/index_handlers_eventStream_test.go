package media

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestSendHealthPing_DeadlineErrorPropagates(t *testing.T) {
	idx := &Indexer{pingWriteTimeout: 10 * time.Millisecond}

	// Use a real websocket conn so we can deterministically make SetWriteDeadline fail.
	ts := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		_ = ws.Close() // force underlying conn closed
		if err := idx.sendHealthPing(ws); err == nil {
			t.Errorf("expected error")
		}
	}))
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	c, err := websocket.Dial(url, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	_ = c.Close()
}

func TestHeartbeatLoop_DisconnectOnErrChan(t *testing.T) {
	idx := &Indexer{heartbeatInterval: 10 * time.Millisecond}

	ts := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		pongCh := make(chan struct{})
		errCh := make(chan error, 1)
		errCh <- net.ErrClosed
		idx.heartbeatLoop(ws, pongCh, errCh)
	}))
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	c, err := websocket.Dial(url, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	_ = c.Close()
}

func TestHeartbeatLoop_PongTimeoutTriggersDisconnect(t *testing.T) {
	idx := &Indexer{
		heartbeatInterval: 10 * time.Millisecond,
		pongTimeout:       20 * time.Millisecond,
	}
	serverDone := make(chan struct{})

	ts := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer close(serverDone)
		defer ws.Close()
		pongCh := make(chan struct{})
		errCh := make(chan error, 1)
		idx.heartbeatLoop(ws, pongCh, errCh)
	}))
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	c, err := websocket.Dial(url, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer c.Close()

	select {
	case <-serverDone:
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for heartbeat loop to exit")
	}
}

func TestHeartbeatLoop_PongKeepsAliveThenErrExits(t *testing.T) {
	idx := &Indexer{
		heartbeatInterval: 15 * time.Millisecond,
		pongTimeout:       50 * time.Millisecond,
	}
	serverDone := make(chan struct{})

	ts := httptest.NewServer(websocket.Handler(func(ws *websocket.Conn) {
		defer close(serverDone)
		defer ws.Close()
		pongCh := make(chan struct{}, 10)
		errCh := make(chan error, 1)

		go func() {
			// Provide pong shortly after each expected ping.
			// Then send an error to stop the loop.
			tk := time.NewTicker(idx.heartbeatInterval)
			defer tk.Stop()
			for i := 0; i < 2; i++ {
				<-tk.C
				time.Sleep(2 * time.Millisecond)
				pongCh <- struct{}{}
			}
			errCh <- net.ErrClosed
		}()

		idx.heartbeatLoop(ws, pongCh, errCh)
	}))
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http")
	c, err := websocket.Dial(url, "", "http://localhost/")
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer c.Close()

	select {
	case <-serverDone:
		// ok
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for heartbeat loop to exit")
	}
}
