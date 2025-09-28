package logging

import (
	"io"
	"log/slog"
	"os"
	"sync/atomic"
)

// Global structured logger. Initialized with a reasonable text handler.
var logger atomic.Pointer[slog.Logger]

func init() {
	l := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Store(l)
}

// L returns the current global logger.
func L() *slog.Logger { return logger.Load() }

// Set replaces the global logger.
func Set(l *slog.Logger) {
	if l != nil {
		logger.Store(l)
	}
}

// New creates a new logger with given level, format ("text" or "json"), and optional writer (defaults stderr).
func New(format string, level slog.Leveler, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	var h slog.Handler
	switch format {
	case "json":
		h = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	default:
		h = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	}
	return slog.New(h)
}
