package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

func startMetricsLogger(ctx context.Context, interval time.Duration, l *slog.Logger, wg *sync.WaitGroup) {
	if interval <= 0 {
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				snap := metrics.Snap()
				l.Info("metrics_snapshot",
					"serial_rx", snap.SerialRx,
					"socketcan_rx", snap.SocketCANRx,
					"serial_tx", snap.SerialTx,
					"socketcan_tx", snap.SocketCANTx,
					"tcp_rx", snap.TCPRx,
					"tcp_tx", snap.TCPTx,
					"hub_drops", snap.HubDrops,
					"errors", snap.Errors,
				)
			case <-ctx.Done():
				return
			}
		}
	}()
}
