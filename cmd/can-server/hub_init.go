package main

import (
	"log/slog"

	"github.com/kstaniek/go-ampio-server/internal/hub"
)

func initHub(cfg *appConfig, l *slog.Logger) *hub.Hub {
	h := hub.New()
	h.OutBufSize = cfg.hubBuffer
	switch cfg.hubPolicy {
	case "drop":
		h.Policy = hub.PolicyDrop
	case "kick":
		h.Policy = hub.PolicyKick
	default:
		l.Warn("unknown_hub_policy", "policy", cfg.hubPolicy, "used", "drop")
		h.Policy = hub.PolicyDrop
	}
	policyStr := map[hub.BackpressurePolicy]string{hub.PolicyDrop: "drop", hub.PolicyKick: "kick"}[h.Policy]
	l.Info("build_info", "version", version, "commit", commit, "date", date)
	l.Info("hub_config", "policy", policyStr, "buffer", h.OutBufSize)
	return h
}
