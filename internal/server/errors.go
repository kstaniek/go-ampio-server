package server

import (
	"errors"

	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

// Sentinel errors used for wrapping so callers can classify via errors.Is.
var (
	ErrListen    = errors.New("listen")
	ErrAccept    = errors.New("accept")
	ErrHandshake = errors.New("handshake")
	ErrConnRead  = errors.New("conn_read")
	ErrConnWrite = errors.New("conn_write")
	ErrBackendTx = errors.New("backend_tx")
	ErrContext   = errors.New("context_cancelled")
)

// mapErrToMetric maps wrapped sentinel errors to metrics labels.
func mapErrToMetric(err error) string {
	switch {
	case errors.Is(err, ErrConnRead):
		return metrics.ErrTCPRead
	case errors.Is(err, ErrConnWrite):
		return metrics.ErrTCPWrite
	case errors.Is(err, ErrHandshake):
		return metrics.ErrHandshake
	case errors.Is(err, ErrBackendTx):
		return metrics.ErrSerialWrite
	case errors.Is(err, ErrAccept), errors.Is(err, ErrListen):
		return metrics.ErrTCPRead
	case errors.Is(err, ErrContext):
		return "context"
	default:
		return "other"
	}
}
