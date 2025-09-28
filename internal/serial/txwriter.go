package serial

import (
	"context"
	"errors"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/logging"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/transport"
)

var ErrTxOverflow = errors.New("serial tx overflow")

// TXWriter funnels all serial writes through one goroutine.
type TXWriter struct{ base *transport.AsyncTx }

// NewTXWriter creates a serial TXWriter with a buffered channel of size buf.
func NewTXWriter(parent context.Context, sp Port, codec Codec, buf int) *TXWriter {
	send := func(fr can.Frame) error {
		_, err := sp.Write(codec.Encode(fr))
		return err
	}
	hooks := transport.Hooks{
		OnError: func(err error) {
			metrics.IncError(metrics.ErrSerialWrite)
			logging.L().Error("serial_write_error", "error", err)
		},
		OnAfter: func() { metrics.IncSerialTx() },
		OnDrop: func() error {
			metrics.IncError(metrics.ErrSerialOverflow)
			return ErrTxOverflow
		},
	}
	return &TXWriter{base: transport.NewAsyncTx(parent, buf, send, hooks)}
}

// SendFrame queues a frame for asynchronous write (drops with ErrTxOverflow if buffer full).
func (w *TXWriter) SendFrame(fr can.Frame) error { return w.base.SendFrame(fr) }

// Close stops the writer and waits for pending goroutine exit.
func (w *TXWriter) Close() { w.base.Close() }
