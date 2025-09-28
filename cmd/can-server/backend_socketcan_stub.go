//go:build !linux

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
)

// Placeholder so non-linux builds compile; socketcan not supported.
func initSocketCANBackend(ctx context.Context, cfg *appConfig, h *hub.Hub, l *slog.Logger, wg *sync.WaitGroup) (func(can.Frame) error, func(), error) {
	return nil, func() {}, fmt.Errorf("socketcan backend unsupported on this platform")
}
