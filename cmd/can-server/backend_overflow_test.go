package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/serial"
)

// blockingPort simulates a very slow serial port to force TX queue overflow.
type blockingPort struct{ block chan struct{} }

func (p *blockingPort) Read(b []byte) (int, error) {
	time.Sleep(5 * time.Millisecond)
	return 0, io.EOF
}
func (p *blockingPort) Write(b []byte) (int, error) { <-p.block; return len(b), nil }
func (p *blockingPort) Close() error                { close(p.block); return nil }

func TestSerialBackendTxOverflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bp := &blockingPort{block: make(chan struct{})}
	openSerialPort = func(name string, baud int, to time.Duration) (serial.Port, error) { return bp, nil }
	defer func() { openSerialPort = serial.Open }()
	beforeErrs := metrics.Snap().Errors

	h := hub.New()
	cfg := &appConfig{backend: "serial", serialDev: "fake", baud: 115200, serialReadTO: 10 * time.Millisecond}
	var wg sync.WaitGroup
	send, cleanup, err := initSerialBackend(ctx, cfg, h, testLogger(), &wg)
	if err != nil {
		t.Fatalf("initSerialBackend: %v", err)
	}
	defer cleanup()

	// Fill buffer; first frame enqueues and worker blocks on Write (channel empty since Write blocks).
	var overflowErr error
	for i := 0; i < txQueueSize+2; i++ {
		fr := can.Frame{CANID: uint32(i)}
		err := send(fr)
		if err != nil && overflowErr == nil {
			overflowErr = err
		}
	}
	if overflowErr == nil {
		t.Fatalf("expected at least one overflow error")
	}
	if !errors.Is(overflowErr, serial.ErrTxOverflow) {
		t.Fatalf("expected ErrTxOverflow, got %v", overflowErr)
	}
	afterErrs := metrics.Snap().Errors
	if afterErrs == beforeErrs {
		t.Fatalf("expected error metric increment on overflow")
	}
}
