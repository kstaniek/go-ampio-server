package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/grandcat/zeroconf"
)

// startMDNS registers the service via mDNS and returns a cleanup function.
// It is safe to call even if disabled (no-op).
const mdnsServiceType = "_can-server._tcp"

func startMDNS(ctx context.Context, cfg *appConfig, port int) (func(), error) {
	if !cfg.mdnsEnable {
		return func() {}, nil
	}
	instance := cfg.mdnsName
	if instance == "" {
		host, _ := os.Hostname()
		instance = fmt.Sprintf("can-server-%s", host)
	}
	meta := []string{
		"backend=" + cfg.backend,
		"version=" + version,
		"commit=" + commit,
	}
	// Hardcoded service type; domain local.
	svc, err := zeroconf.Register(instance, mdnsServiceType, "local.", port, meta, nil)
	if err != nil {
		return nil, fmt.Errorf("mdns register: %w", err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		svc.Shutdown()
	}()
	return func() { close(done); svc.Shutdown(); time.Sleep(50 * time.Millisecond) }, nil
}
