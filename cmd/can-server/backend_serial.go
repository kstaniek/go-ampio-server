package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/serial"
)

// sleepFn allows tests to intercept backoff sleeps.
var sleepFn = time.Sleep

// openSerialPort is a hook for tests (overridden in unit tests).
var openSerialPort = serial.Open

// initSerialBackend sets up the serial backend, launching the RX loop.
func initSerialBackend(ctx context.Context, cfg *appConfig, h *hub.Hub, l *slog.Logger, wg *sync.WaitGroup) (func(can.Frame) error, func(), error) {
	sp, err := openSerialPort(cfg.serialDev, cfg.baud, cfg.serialReadTO)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open serial: %w", err)
	}
	l.Info("serial_open", "device", cfg.serialDev, "baud", cfg.baud)
	serCodec := serial.Codec{}
	w := serial.NewTXWriter(ctx, sp, serCodec, txQueueSize)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer l.Info("serial_rx_end")
		buf := make([]byte, serialReadBufSize)
		acc := bytes.NewBuffer(nil)
		backoff := rxBackoffMin
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := sp.Read(buf)
			if n > 0 {
				acc.Write(buf[:n])
				_ = serCodec.DecodeStream(acc, func(fr can.Frame) { h.Broadcast(fr) })
				if acc.Len() == 0 && cap(acc.Bytes()) > largeBufferReclaimThreshold {
					acc = bytes.NewBuffer(nil)
				}
				backoff = rxBackoffMin
			}
			if err != nil {
				if ctx.Err() != nil { // shutting down
					return
				}
				var perr *os.PathError
				if errors.As(err, &perr) {
					return // device removed or fatal
				}
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					continue // ignore transient EOF
				}
				metrics.IncError(metrics.ErrSerialRead)
				l.Warn("serial_read_error", "error", err, "backoff", backoff)
				sleepFn(backoff)
				backoff *= 2
				if backoff > rxBackoffMax {
					backoff = rxBackoffMax
				}
			}
		}
	}()
	return w.SendFrame, func() { _ = sp.Close(); w.Close() }, nil
}
