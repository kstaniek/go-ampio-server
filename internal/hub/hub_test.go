package hub

import (
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

func TestHub_Broadcast_DropDoesNotBlock(t *testing.T) {
	h := New()
	// If your Hub doesn't expose OutBufSize/Policy, we can still test behavior directly.
	cl := &Client{Out: make(chan can.Frame, 4), Closed: make(chan struct{})}
	h.Add(cl)
	defer h.Remove(cl)

	// Don't read from cl.Out to simulate slow client
	start := time.Now()
	for i := 0; i < 1000; i++ {
		h.Broadcast(can.Frame{CANID: 0x123 | 0x80000000})
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Fatalf("Broadcast took too long: %s", elapsed)
	}
	// Buffer should be full
	if len(cl.Out) != cap(cl.Out) {
		t.Fatalf("expected client buffer to be full, got len=%d cap=%d", len(cl.Out), cap(cl.Out))
	}
}

func TestHub_Broadcast_DropKeepsOthersFlowing(t *testing.T) {
	h := New()
	slow := &Client{Out: make(chan can.Frame, 1), Closed: make(chan struct{})}
	fast := &Client{Out: make(chan can.Frame, 16), Closed: make(chan struct{})}
	h.Add(slow)
	h.Add(fast)
	defer h.Remove(slow)
	defer h.Remove(fast)

	// Fill slow buffer
	h.Broadcast(can.Frame{CANID: 0x1 | 0x80000000})
	select {
	case <-slow.Out:
		// shouldn't happen; we intentionally don't read
	default:
	}

	// Now send bursts that would drop on slow but must be delivered to fast
	for i := 0; i < 10; i++ {
		h.Broadcast(can.Frame{CANID: 0x2 | 0x80000000})
	}

	got := 0
	timeout := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case <-fast.Out:
			got++
			if got >= 5 { // at least some got through
				break loop
			}
		case <-timeout:
			break loop
		}
	}
	if got == 0 {
		t.Fatalf("fast client did not receive any frames while slow was backpressured")
	}
}
