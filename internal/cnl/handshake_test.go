package cnl

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestHandshakeLoopback(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()

	ctx := context.Background()

	done := make(chan error, 1)
	go func() { done <- Handshake(ctx, srv, 2*time.Second) }()

	if err := Handshake(ctx, cli, 2*time.Second); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
}
