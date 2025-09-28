package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/serial"
	"github.com/kstaniek/go-ampio-server/internal/socketcan"
)

func (s *Server) startReader(ctxDone <-chan struct{}, conn net.Conn, cl *hub.Client, logger *slog.Logger) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { _ = conn.Close() }()
		for {
			_ = conn.SetReadDeadline(time.Now().Add(s.readDeadline))
			var count int
			if mfd, ok := s.Codec.(interface {
				DecodeN(io.Reader, int, func(can.Frame)) (int, error)
			}); ok {
				var err error
				count, err = mfd.DecodeN(conn, 16, func(fr can.Frame) {
					if s.frameFilter != nil && !s.frameFilter(&fr) {
						return
					}
					metrics.IncTCPRx()
					if err := s.Send(fr); err != nil {
						if errors.Is(err, serial.ErrTxOverflow) || errors.Is(err, socketcan.ErrTxOverflow) {
							s.totalBackendOverflow.Add(1)
							logger.Debug("backend_overflow_drop", "can_id", fmt.Sprintf("0x%X", fr.CANID), "len", fr.Len)
						} else {
							s.totalBackendErrors.Add(1)
							logger.Error("backend_tx_error", "error", err, "can_id", fmt.Sprintf("0x%X", fr.CANID))
						}
					}
				})
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
						return
					}
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue
					}
					wrap := fmt.Errorf("%w: %v", ErrConnRead, err)
					metrics.IncError(mapErrToMetric(wrap))
					s.setError(wrap)
					return
				}
			} else {
				fr, err := s.Codec.Decode(conn)
				if err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
						return
					}
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue
					}
					wrap := fmt.Errorf("%w: %v", ErrConnRead, err)
					metrics.IncError(mapErrToMetric(wrap))
					s.setError(wrap)
					return
				}
				if s.frameFilter == nil || s.frameFilter(&fr) {
					metrics.IncTCPRx()
					if err := s.Send(fr); err != nil {
						if errors.Is(err, serial.ErrTxOverflow) || errors.Is(err, socketcan.ErrTxOverflow) {
							s.totalBackendOverflow.Add(1)
							logger.Debug("backend_overflow_drop", "can_id", fmt.Sprintf("0x%X", fr.CANID), "len", fr.Len)
						} else {
							wrap := fmt.Errorf("%w: %v", ErrBackendTx, err)
							s.setError(wrap)
							s.totalBackendErrors.Add(1)
							logger.Error("backend_tx_error", "error", wrap, "can_id", fmt.Sprintf("0x%X", fr.CANID))
						}
					}
				}
				count = 1
			}
			if count == 0 {
				time.Sleep(100 * time.Microsecond)
			}
			select {
			case <-ctxDone:
				return
			default:
			}
		}
	}()
}
