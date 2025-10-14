package transport

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

var (
	errOverflow = errors.New("overflow")
	errSendFail = errors.New("send fail")
)

// TestAsyncTxSuccess verifies frames are sent and hooks fire.
func TestAsyncTxSuccess(t *testing.T) {
	var sent atomic.Int64
	var after atomic.Int64
	ax := NewAsyncTx(context.Background(), 4, func(fr can.Frame) error {
		sent.Add(1)
		return nil
	}, Hooks{OnAfter: func() { after.Add(1) }})
	defer ax.Close()
	for i := 0; i < 3; i++ {
		if err := ax.SendFrame(can.Frame{CANID: uint32(i), Len: 0}); err != nil {
			t.Fatalf("unexpected send error: %v", err)
		}
	}
	// Allow worker to drain
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && sent.Load() < 3 {
		time.Sleep(5 * time.Millisecond)
	}
	if sent.Load() != 3 || after.Load() != 3 {
		t.Fatalf("expected 3 sent & after, got sent=%d after=%d", sent.Load(), after.Load())
	}
}

// TestAsyncTxOverflow ensures OnDrop is invoked when buffer full.
func TestAsyncTxOverflow(t *testing.T) {
	// Slow send function blocks until context cancelled -> fill buffer quickly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var drops atomic.Int64
	ax := NewAsyncTx(ctx, 1, func(fr can.Frame) error { time.Sleep(150 * time.Millisecond); return nil }, Hooks{OnDrop: func() error { drops.Add(1); return errOverflow }})
	defer ax.Close()
	// First frame enqueued.
	if err := ax.SendFrame(can.Frame{}); err != nil {
		t.Fatalf("unexpected error enqueue first: %v", err)
	}
	// Immediate second should overflow (buffer=1, worker sleeping)
	if err := ax.SendFrame(can.Frame{}); !errors.Is(err, errOverflow) {
		t.Fatalf("expected overflow error, got %v", err)
	}
	if drops.Load() != 1 {
		t.Fatalf("expected 1 drop, got %d", drops.Load())
	}
}

// TestAsyncTxSendError triggers OnError hook.
func TestAsyncTxSendError(t *testing.T) {
	var errs atomic.Int64
	ax := NewAsyncTx(context.Background(), 2, func(fr can.Frame) error { return errSendFail }, Hooks{OnError: func(error) { errs.Add(1) }})
	defer ax.Close()
	_ = ax.SendFrame(can.Frame{})
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && errs.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if errs.Load() == 0 {
		t.Fatalf("expected error hook invocation")
	}
}

// TestAsyncTxClose stops processing further frames.
func TestAsyncTxClose(t *testing.T) {
	var sent atomic.Int64
	ax := NewAsyncTx(context.Background(), 2, func(fr can.Frame) error { sent.Add(1); return nil }, Hooks{})
	_ = ax.SendFrame(can.Frame{})
	ax.Close()
	countAfterClose := sent.Load()
	// Try sending after close (undefined but should not panic or increment)
	_ = ax.SendFrame(can.Frame{})
	// Give some time in case worker erroneously processed second frame.
	time.Sleep(50 * time.Millisecond)
	if sent.Load() != countAfterClose {
		t.Fatalf("frame processed after close: before=%d after=%d", countAfterClose, sent.Load())
	}
}

func TestAsyncTxSendAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx := NewAsyncTx(ctx, 2, func(fr can.Frame) error { return nil }, Hooks{})
	tx.Close()
	if err := tx.SendFrame(can.Frame{CANID: 123}); !errors.Is(err, ErrAsyncTxClosed) {
		t.Fatalf("expected ErrAsyncTxClosed, got %v", err)
	}
}

func TestAsyncTxCloseConcurrentSend(t *testing.T) {
	for i := 0; i < 100; i++ {
		ax := NewAsyncTx(context.Background(), 1, func(fr can.Frame) error { return nil }, Hooks{})
		done := make(chan error, 1)
		go func() {
			done <- ax.SendFrame(can.Frame{})
		}()
		time.Sleep(1 * time.Millisecond)
		ax.Close()
		if err := <-done; err != nil && !errors.Is(err, ErrAsyncTxClosed) {
			t.Fatalf("iteration %d: unexpected send error %v", i, err)
		}
	}
}
