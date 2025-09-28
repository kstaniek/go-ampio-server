package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/cnl"
	"github.com/kstaniek/go-ampio-server/internal/hub"
)

// mockSend is a no-op backend send function.
func mockSend(can.Frame) error { return nil }

// startInMemoryServer launches the server on :0 for benchmarks.
func startInMemoryServer(b *testing.B, h *hub.Hub) (*Server, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(mockSend))
	go func() { _ = srv.Serve(ctx) }()
	select {
	case <-srv.Ready():
	case <-time.After(time.Second):
		b.Fatalf("server not ready")
	}
	return srv, cancel
}

func BenchmarkServerWriterFlush(b *testing.B) {
	h := hub.New()
	h.OutBufSize = 0
	srv, cancel := startInMemoryServer(b, h)
	defer cancel()
	// Dial the server
	conn, err := net.Dial("tcp", srv.Addr())
	if err != nil {
		b.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	// Perform handshake manually
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte("CANNELLONIv1")); err != nil {
		b.Fatalf("handshake write: %v", err)
	}
	buf := make([]byte, len("CANNELLONIv1"))
	if _, err := conn.Read(buf); err != nil {
		b.Fatalf("handshake read: %v", err)
	}

	// Add a client to hub (simulate broadcast direction)
	cl := &hub.Client{Out: make(chan can.Frame, 1024), Closed: make(chan struct{})}
	h.Add(cl)
	// Feed frames into client channel; the server writer loop should consume.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cl.Out <- can.Frame{CANID: uint32(i), Len: 0}
	}
	b.StopTimer()
	close(cl.Closed)
}
