package server

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

// startWriter launches the goroutine pushing hub frames to a single client connection.
func (s *Server) startWriter(ctxDone <-chan struct{}, conn net.Conn, cl *hub.Client, logger *slog.Logger) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			_ = conn.Close()
			if s.Hub != nil {
				s.Hub.Remove(cl)
			}
			s.totalDisconnected.Add(1)
			logger.Info("client_disconnected")
		}()
		t := time.NewTicker(s.flushInterval)
		defer t.Stop()
		batch := make([]can.Frame, 0, s.batchSize)
		flush := func() error {
			if len(batch) == 0 {
				return nil
			}
			n := len(batch)
			if beTo, ok := s.Codec.(interface {
				EncodeTo(io.Writer, []can.Frame) (int, error)
			}); ok {
				_, err := beTo.EncodeTo(conn, batch)
				batch = batch[:0]
				if err != nil {
					wrap := fmt.Errorf("%w: %v", ErrConnWrite, err)
					metrics.IncError(mapErrToMetric(wrap))
					s.setError(wrap)
					return wrap
				}
				metrics.AddTCPTx(n)
				return nil
			}
			var payload []byte
			if be, ok := s.Codec.(interface{ Encode([]can.Frame) []byte }); ok {
				payload = be.Encode(batch)
			}
			batch = batch[:0]
			if _, err := conn.Write(payload); err != nil {
				wrap := fmt.Errorf("%w: %v", ErrConnWrite, err)
				metrics.IncError(mapErrToMetric(wrap))
				s.setError(wrap)
				return wrap
			}
			metrics.AddTCPTx(n)
			return nil
		}
		for {
			select {
			case fr := <-cl.Out:
				batch = append(batch, fr)
				if len(batch) >= s.batchSize {
					if err := flush(); err != nil {
						return
					}
				}
			case <-t.C:
				if err := flush(); err != nil {
					return
				}
			case <-cl.Closed:
				_ = flush()
				return
			case <-ctxDone:
				_ = flush()
				return
			}
		}
	}()
}

// (writer specific helpers only)
