//go:build linux

package socketcan

import (
	"context"
	"errors"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/transport"
)

var ErrTxOverflow = errors.New("socketcan tx overflow")

// Dev is the minimal interface needed by the backend and TXWriter.
// Implemented by *Device in production and by fakes in tests.
type Dev interface {
	ReadFrame(*can.Frame) error
	WriteFrame(can.Frame) error
	Close() error
}

// TXWriter funnels all SocketCAN writes through a single goroutine,
// mirroring the serial TXWriter behavior.
type TXWriter struct{ base *transport.AsyncTx }

// NewTXWriter creates a SocketCAN TXWriter with a buffered channel of size buf.
func NewTXWriter(parent context.Context, dev Dev, buf int) *TXWriter {
	send := func(fr can.Frame) error { return dev.WriteFrame(fr) }
	hooks := transport.Hooks{
		OnError: func(err error) { metrics.IncError(metrics.ErrSocketCANWrite) },
		OnAfter: func() { metrics.IncSocketCANTx() },
		OnDrop: func() error {
			metrics.IncError(metrics.ErrSocketCANOver)
			return ErrTxOverflow
		},
	}
	return &TXWriter{base: transport.NewAsyncTx(parent, buf, send, hooks)}
}

// SendFrame queues a frame for asynchronous device write (drops with ErrTxOverflow if buffer full).
func (w *TXWriter) SendFrame(fr can.Frame) error { return w.base.SendFrame(fr) }

// Close stops the writer and waits for the worker goroutine to finish.
func (w *TXWriter) Close() { w.base.Close() }
