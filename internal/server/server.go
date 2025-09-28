package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/logging"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/transport"
)

// SendFunc transmits a CAN frame to the selected backend device.
type SendFunc func(can.Frame) error

// Server owns the TCP listener and coordinates client lifecycle.
type Server struct {
	mu    sync.RWMutex
	addr  string
	Hub   *hub.Hub
	Codec transport.FrameDecoder // *cnl.Codec implements
	Send  SendFunc

	frameFilter func(*can.Frame) bool

	flushInterval        time.Duration
	batchSize            int
	readDeadline         time.Duration
	handshakeTimeout     time.Duration
	maxClients           int
	readyOnce            sync.Once
	readyCh              chan struct{}
	lastErrMu            sync.Mutex
	lastErr              error
	errCh                chan error
	listener             net.Listener
	clientsMu            sync.RWMutex
	clients              map[*hub.Client]net.Conn
	wg                   sync.WaitGroup
	logger               *slog.Logger
	nextConnID           uint64
	totalAccepted        atomic.Uint64
	totalHandshakeFail   atomic.Uint64
	totalConnected       atomic.Uint64
	totalDisconnected    atomic.Uint64
	totalBackendOverflow atomic.Uint64
	totalBackendErrors   atomic.Uint64
}

const (
	defaultFlushInterval    = 5 * time.Millisecond
	defaultBatchSize        = 64
	defaultReadDeadline     = 60 * time.Second
	defaultHandshakeTimeout = 3 * time.Second
)

type ServerOption func(*Server)

func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		flushInterval:    defaultFlushInterval,
		batchSize:        defaultBatchSize,
		readDeadline:     defaultReadDeadline,
		handshakeTimeout: defaultHandshakeTimeout,
		readyCh:          make(chan struct{}),
		errCh:            make(chan error, 1),
		clients:          make(map[*hub.Client]net.Conn),
		logger:           logging.L(),
	}
	for _, o := range opts {
		o(s)
	}
	if s.addr == "" {
		s.addr = ":0"
	}
	return s
}

func WithListenAddr(a string) ServerOption            { return func(s *Server) { s.addr = a } }
func WithHub(hb *hub.Hub) ServerOption                { return func(s *Server) { s.Hub = hb } }
func WithCodec(c transport.FrameDecoder) ServerOption { return func(s *Server) { s.Codec = c } }
func WithSend(send SendFunc) ServerOption             { return func(s *Server) { s.Send = send } }
func WithFrameFilter(fn func(*can.Frame) bool) ServerOption {
	return func(s *Server) { s.frameFilter = fn }
}

func WithFlushInterval(d time.Duration) ServerOption {
	return func(s *Server) {
		if d > 0 {
			s.flushInterval = d
		}
	}
}

func WithBatchSize(n int) ServerOption {
	return func(s *Server) {
		if n > 0 {
			s.batchSize = n
		}
	}
}

func WithReadDeadline(d time.Duration) ServerOption {
	return func(s *Server) {
		if d > 0 {
			s.readDeadline = d
		}
	}
}

func WithHandshakeTimeout(d time.Duration) ServerOption {
	return func(s *Server) {
		if d > 0 {
			s.handshakeTimeout = d
		}
	}
}

func WithMaxClients(n int) ServerOption {
	return func(s *Server) {
		if n > 0 {
			s.maxClients = n
		}
	}
}

func WithLogger(l *slog.Logger) ServerOption {
	return func(s *Server) {
		if l != nil {
			s.logger = l
		}
	}
}

func (s *Server) Addr() string           { s.mu.RLock(); defer s.mu.RUnlock(); return s.addr }
func (s *Server) setAddr(a string)       { s.mu.Lock(); s.addr = a; s.mu.Unlock() }
func (s *Server) SetListenAddr(a string) { s.setAddr(a) }
func (s *Server) Ready() <-chan struct{} { return s.readyCh }
func (s *Server) Errors() <-chan error   { return s.errCh }

func (s *Server) setError(err error) {
	if err == nil {
		return
	}
	s.lastErrMu.Lock()
	s.lastErr = err
	s.lastErrMu.Unlock()
	select {
	case s.errCh <- err:
	default:
	}
}
func (s *Server) LastError() error { s.lastErrMu.Lock(); defer s.lastErrMu.Unlock(); return s.lastErr }

// Serve accepts TCP clients and spawns reader/writer goroutines.
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	addr := s.addr
	if addr == "" {
		addr = ":0"
	}
	s.mu.Unlock()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		wrap := fmt.Errorf("%w: %v", ErrListen, err)
		metrics.IncError(mapErrToMetric(wrap))
		s.setError(wrap)
		return wrap
	}
	s.setAddr(ln.Addr().String())
	s.listener = ln
	if s.readyCh != nil {
		s.readyOnce.Do(func() { close(s.readyCh) })
	}
	s.logger.Info("tcp_listen", "addr", s.Addr())
	s.logger.Info("ready")
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		if err := s.acceptOnce(ctx, ln); err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// acceptOnce accepts a single connection, performs handshake, registers client and spawns IO goroutines.
// Returns nil on success; a wrapped error on fatal listener errors.
func (s *Server) acceptOnce(ctx context.Context, ln net.Listener) error {
	conn, err := ln.Accept()
	if err != nil {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}
		if _, ok := err.(net.Error); ok { // transient
			time.Sleep(200 * time.Millisecond)
			return nil
		}
		wrap := fmt.Errorf("%w: %v", ErrAccept, err)
		metrics.IncError(mapErrToMetric(wrap))
		s.setError(wrap)
		return wrap
	}
	s.totalAccepted.Add(1)
	connID := atomic.AddUint64(&s.nextConnID, 1)
	connLogger := s.logger.With("conn_id", connID, "remote", conn.RemoteAddr().String())
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
	if err := s.CannelloniHandshake(ctx, conn); err != nil {
		wrap := fmt.Errorf("%w: %v", ErrHandshake, err)
		metrics.IncError(mapErrToMetric(wrap))
		s.setError(wrap)
		s.totalHandshakeFail.Add(1)
		connLogger.Warn("handshake_failed", "error", wrap)
		_ = conn.Close()
		return nil
	}
	if s.maxClients > 0 && s.Hub != nil && s.Hub.Count() >= s.maxClients {
		metrics.IncHubReject()
		connLogger.Warn("client_reject_max", "max_clients", s.maxClients)
		_ = conn.Close()
		return nil
	}
	client := s.newClient()
	s.clientsMu.Lock()
	s.clients[client] = conn
	s.clientsMu.Unlock()
	s.totalConnected.Add(1)
	connLogger.Info("client_connected")
	s.startWriter(ctx.Done(), conn, client, connLogger)
	s.startReader(ctx.Done(), conn, client, connLogger)
	return nil
}

// newClient allocates a hub client with buffer size derived from hub config.
func (s *Server) newClient() *hub.Client {
	bufSize := 512
	if s.Hub != nil && s.Hub.OutBufSize > 0 {
		bufSize = s.Hub.OutBufSize
	}
	cl := &hub.Client{Out: make(chan can.Frame, bufSize), Closed: make(chan struct{})}
	if s.Hub != nil {
		s.Hub.Add(cl)
		metrics.SetHubClients(s.Hub.Count())
	}
	return cl
}

// Shutdown gracefully closes all resources.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	ln := s.listener
	s.listener = nil
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	s.clientsMu.Lock()
	for cl, conn := range s.clients {
		_ = conn.Close()
		if s.Hub != nil {
			s.Hub.Remove(cl)
		}
		delete(s.clients, cl)
	}
	s.clientsMu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: shutdown timeout: %v", ErrContext, ctx.Err())
	case <-done:
		s.logger.Info("shutdown_summary", "accepted", s.totalAccepted.Load(), "handshake_fail", s.totalHandshakeFail.Load(), "connected", s.totalConnected.Load(), "disconnected", s.totalDisconnected.Load(), "backend_overflow", s.totalBackendOverflow.Load(), "backend_errors", s.totalBackendErrors.Load())
		return nil
	}
}
