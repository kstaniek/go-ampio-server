package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
)

// initBackend selects the backend, starts its RX loop and returns a frame sender and cleanup.
// It returns an error instead of exiting the process to allow graceful handling by the caller.
func initBackend(ctx context.Context, cfg *appConfig, h *hub.Hub, l *slog.Logger, wg *sync.WaitGroup) (func(can.Frame) error, func(), error) {
	switch cfg.backend {
	case "serial":
		return initSerialBackend(ctx, cfg, h, l, wg)
	case "socketcan":
		return initSocketCANBackend(ctx, cfg, h, l, wg)
	default:
		return nil, func() {}, fmt.Errorf("unknown backend %q (use serial|socketcan)", cfg.backend)
	}
}
