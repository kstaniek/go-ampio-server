//go:build linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/socketcan"
)

// openSocketCANDevice is a hook for tests (overridden in unit tests).
var openSocketCANDevice = func(iface string) (socketcan.Dev, error) { return socketcan.Open(iface) }

// initSocketCANBackend sets up the SocketCAN backend, launching the RX loop.
func initSocketCANBackend(ctx context.Context, cfg *appConfig, h *hub.Hub, l *slog.Logger, wg *sync.WaitGroup) (func(can.Frame) error, func(), error) {
	dev, err := openSocketCANDevice(cfg.canIf)
	if err != nil {
		return nil, func() {}, fmt.Errorf("socketcan open %s: %w", cfg.canIf, err)
	}
	l.Info("socketcan_open", "if", cfg.canIf)
	tw := socketcan.NewTXWriter(ctx, dev, txQueueSize)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer l.Info("socketcan_rx_end")
		backoff := rxBackoffMin
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			var fr can.Frame
			if err := dev.ReadFrame(&fr); err != nil {
				if ctx.Err() != nil { // shutting down
					return
				}
				metrics.IncError(metrics.ErrSocketCANRead)
				l.Warn("socketcan_read_error", "error", err, "backoff", backoff)
				sleepFn(backoff)
				backoff *= 2
				if backoff > rxBackoffMax {
					backoff = rxBackoffMax
				}
				continue
			}
			metrics.IncSocketCANRx()
			h.Broadcast(fr)
			backoff = rxBackoffMin
		}
	}()
	return tw.SendFrame, func() { _ = dev.Close(); tw.Close() }, nil
}
