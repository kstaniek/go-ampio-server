package transport

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

// AsyncTx is a reusable asynchronous frame transmitter that funnels frame
// writes through a single goroutine (fan-in). It provides non-blocking enqueue
// semantics: if the internal buffer is full, SendFrame invokes the configured
// OnDrop hook and returns its error (usually an overflow sentinel). This keeps
// producers from blocking behind a slow or wedged device/backend and matches
// the pre-refactor behavior of the serial and SocketCAN writers.
//
// Life-cycle:
//
//	a := NewAsyncTx(ctx, buf, sendFn, hooks)
//	a.SendFrame(frame)
//	a.Close()
//
// After Close returns no more frames will be processed, but (by design) the
// channel is not closed; additional SendFrame calls will enqueue (or drop) but
// have no effect because the worker has exited. Callers should not send after
// Close; this mirrors the previous concrete writers. If stricter semantics are
// required we could add an internal atomic flag and reject late sends.
//
// Hooks let each backend keep distinct metrics / logging without duplicating
// the goroutine + buffer plumbing.
type AsyncTx struct {
	mu     sync.Mutex
	ch     chan can.Frame
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	send   func(can.Frame) error
	hooks  Hooks
	closed atomic.Bool // set when Close is called; prevents enqueue after shutdown
}

// Hooks customize AsyncTx behavior.
type Hooks struct {
	// OnError is called when send returns a non-nil error (frame not sent).
	OnError func(error)
	// OnAfter is called only after a successful send.
	OnAfter func()
	// OnDrop is called when the buffer is full; its returned error is returned
	// from SendFrame. If nil, the overflow is silent (best-effort fire-and-forget).
	OnDrop func() error
}

// NewAsyncTx constructs an AsyncTx with a buffered channel of size buf.
func NewAsyncTx(parent context.Context, buf int, send func(can.Frame) error, hooks Hooks) *AsyncTx {
	ctx, cancel := context.WithCancel(parent)
	a := &AsyncTx{
		ch:     make(chan can.Frame, buf),
		ctx:    ctx,
		cancel: cancel,
		send:   send,
		hooks:  hooks,
	}
	a.wg.Add(1)
	go a.loop()
	return a
}

func (a *AsyncTx) loop() {
	defer a.wg.Done()
	for {
		select {
		case fr, ok := <-a.ch:
			if !ok { // channel closed
				return
			}
			if err := a.send(fr); err != nil {
				if a.hooks.OnError != nil {
					a.hooks.OnError(err)
				}
				continue
			}
			if a.hooks.OnAfter != nil {
				a.hooks.OnAfter()
			}
		case <-a.ctx.Done():
			return
		}
	}
}

// SendFrame queues a frame for asynchronous transmission or returns the drop
// error if the buffer is full.
var ErrAsyncTxClosed = errors.New("async tx closed")

func (a *AsyncTx) SendFrame(fr can.Frame) error {
	// Fast-path check so steady-state sends avoid taking the lock when already shut down.
	if a.closed.Load() {
		return ErrAsyncTxClosed
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed.Load() {
		return ErrAsyncTxClosed
	}
	select {
	case a.ch <- fr:
		return nil
	default:
		if a.hooks.OnDrop != nil {
			return a.hooks.OnDrop()
		}
		return nil
	}
}

// Close stops the worker and waits for all pending operations to finish.
func (a *AsyncTx) Close() {
	if a.closed.Swap(true) { // already closed
		return
	}
	// Cancel context to stop loop, then close channel under the send lock to avoid races.
	a.cancel()
	a.mu.Lock()
	close(a.ch)
	a.mu.Unlock()
	a.wg.Wait()
}
