package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/kstaniek/go-ampio-server/internal/cnl"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/server"
)

// Helper implementations moved to dedicated files: version.go, config.go, logger.go, hub_init.go, metrics_logger.go, backend.go.

func main() {
	cfg, showVersion := parseFlags()
	if showVersion {
		fmt.Printf("can-server %s (commit %s, built %s)\n", version, commit, date)
		return
	}
	l := setupLogger(cfg.logFormat, cfg.logLevel)
	h := initHub(cfg, l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	startMetricsLogger(ctx, cfg.logMetricsEvery, l, &wg)

	sendFunc, cleanup, berr := initBackend(ctx, cfg, h, l, &wg)
	if berr != nil {
		l.Error("backend_init_error", "error", berr)
		return
	}

	srv := server.NewServer(
		server.WithHub(h),
		server.WithCodec(&cnl.Codec{}),
		server.WithSend(sendFunc),
		server.WithLogger(l),
		server.WithMaxClients(cfg.maxClients),
		server.WithHandshakeTimeout(cfg.handshakeTO),
		server.WithReadDeadline(cfg.clientReadTO),
	)
	srv.SetListenAddr(cfg.listenAddr)
	go func() {
		if err := srv.Serve(ctx); err != nil {
			l.Error("tcp_server_error", "error", err)
			cancel()
		}
	}()

	// Start mDNS advertisement once listener is ready.
	go func() {
		if !cfg.mdnsEnable {
			return
		}
		select {
		case <-srv.Ready():
		case <-ctx.Done():
			return
		}
		// Extract port from bound address (host:port or :port)
		addr := srv.Addr()
		var portNum int
		if _, p, err := net.SplitHostPort(addr); err == nil {
			if pn, perr := strconv.Atoi(p); perr == nil {
				portNum = pn
			}
		}
		if portNum == 0 { // fallback attempt if format unexpected
			lastColon := strings.LastIndex(addr, ":")
			if lastColon >= 0 {
				if pn, perr := strconv.Atoi(addr[lastColon+1:]); perr == nil {
					portNum = pn
				}
			}
		}
		cleanupMDNS, err := startMDNS(ctx, cfg, portNum)
		if err != nil {
			l.Warn("mdns_start_failed", "error", err)
			return
		}
		l.Info("mdns_started", "service", mdnsServiceType, "name", cfg.mdnsName, "port", portNum)
		go func() { <-ctx.Done(); cleanupMDNS() }()
	}()

	// Ready when server listener is bound and context not cancelled.
	metrics.SetReadinessFunc(func() bool {
		select {
		case <-srv.Ready():
		default:
			return false
		}
		return ctx.Err() == nil
	})
	if cfg.metricsAddr != "" {
		metrics.InitBuildInfo(version, commit, date)
		srvHTTP := metrics.StartHTTP(cfg.metricsAddr)
		defer func() { _ = srvHTTP.Shutdown(context.Background()) }()
	}
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	s := <-sigCh
	l.Info("shutdown_signal", "signal", s.String())
	cancel()
	cleanup()
	wg.Wait()
}
