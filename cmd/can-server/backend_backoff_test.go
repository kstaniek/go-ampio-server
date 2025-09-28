package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/serial"
)

// fakeErrPort always returns a synthetic error to trigger backoff.
type fakeErrPort struct{}

func (f *fakeErrPort) Read(p []byte) (int, error)  { return 0, io.ErrNoProgress }
func (f *fakeErrPort) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeErrPort) Close() error                { return nil }

func TestSerialBackendBackoffProgression(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	openSerialPort = func(name string, baud int, to time.Duration) (serial.Port, error) { return &fakeErrPort{}, nil }
	defer func() { openSerialPort = serial.Open }()

	var mu sync.Mutex
	var seen []time.Duration
	sleepFn = func(d time.Duration) {
		mu.Lock()
		if len(seen) < 6 { // capture first few entries
			seen = append(seen, d)
			if len(seen) == 6 {
				cancel()
			}
		}
		mu.Unlock()
	}
	defer func() { sleepFn = time.Sleep }()

	h := hub.New()
	cfg := &appConfig{backend: "serial", serialDev: "fake", baud: 9600, serialReadTO: 10 * time.Millisecond}
	var wg sync.WaitGroup
	_, cleanup, err := initSerialBackend(ctx, cfg, h, slog.Default(), &wg)
	if err != nil {
		t.Fatalf("initSerialBackend: %v", err)
	}
	cleanup()
	wg.Wait()

	if len(seen) < 3 {
		t.Fatalf("expected at least 3 backoff samples, got %d", len(seen))
	}
	// Validate non-decreasing, starts at min, and never exceeds max.
	prev := rxBackoffMin / 4 // allow first comparison
	for i, d := range seen {
		if d < prev {
			t.Fatalf("backoff decreased at %d: prev=%v cur=%v", i, prev, d)
		}
		if d > rxBackoffMax {
			t.Fatalf("backoff exceeded max at %d: %v > %v", i, d, rxBackoffMax)
		}
		prev = d
	}
	if seen[0] != rxBackoffMin {
		t.Fatalf("expected first backoff %v got %v", rxBackoffMin, seen[0])
	}
}
