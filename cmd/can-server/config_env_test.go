package main

import (
	"os"
	"testing"
	"time"
)

func TestApplyEnvOverrides_Basic(t *testing.T) {
	base := &appConfig{
		serialDev:       "/dev/null",
		baud:            115200,
		listenAddr:      ":20000",
		serialReadTO:    50 * time.Millisecond,
		logFormat:       "text",
		logLevel:        "info",
		metricsAddr:     "",
		hubBuffer:       512,
		hubPolicy:       "drop",
		backend:         "socketcan",
		canIf:           "can0",
		maxClients:      0,
		handshakeTO:     3 * time.Second,
		clientReadTO:    60 * time.Second,
		logMetricsEvery: 0,
		mdnsEnable:      false,
		mdnsName:        "",
	}

	// Set env overrides
	os.Setenv("CAN_SERVER_BAUD", "230400")
	os.Setenv("CAN_SERVER_MDNS_ENABLE", "true")
	os.Setenv("CAN_SERVER_SERIAL_READ_TIMEOUT", "100ms")
	os.Setenv("CAN_SERVER_LOG_METRICS_INTERVAL", "5s")
	t.Cleanup(func() {
		os.Unsetenv("CAN_SERVER_BAUD")
		os.Unsetenv("CAN_SERVER_MDNS_ENABLE")
		os.Unsetenv("CAN_SERVER_SERIAL_READ_TIMEOUT")
		os.Unsetenv("CAN_SERVER_LOG_METRICS_INTERVAL")
	})
	if err := applyEnvOverrides(base, map[string]struct{}{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if base.baud != 230400 {
		t.Fatalf("expected baud override, got %d", base.baud)
	}
	if !base.mdnsEnable {
		t.Fatalf("expected mdnsEnable true")
	}
	if base.serialReadTO != 100*time.Millisecond {
		t.Fatalf("expected serialReadTO 100ms got %v", base.serialReadTO)
	}
	if base.logMetricsEvery != 5*time.Second {
		t.Fatalf("expected logMetricsEvery 5s got %v", base.logMetricsEvery)
	}
}

func TestApplyEnvOverrides_FlagPrecedence(t *testing.T) {
	base := &appConfig{baud: 115200}
	os.Setenv("CAN_SERVER_BAUD", "230400")
	t.Cleanup(func() { os.Unsetenv("CAN_SERVER_BAUD") })
	// Simulate user passed -baud flag (so env should be ignored)
	if err := applyEnvOverrides(base, map[string]struct{}{"baud": {}}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if base.baud != 115200 {
		t.Fatalf("expected baud unchanged 115200 got %d", base.baud)
	}
}

func TestApplyEnvOverrides_BadInt(t *testing.T) {
	base := &appConfig{hubBuffer: 512}
	os.Setenv("CAN_SERVER_HUB_BUFFER", "notint")
	t.Cleanup(func() { os.Unsetenv("CAN_SERVER_HUB_BUFFER") })
	if err := applyEnvOverrides(base, map[string]struct{}{}); err == nil {
		t.Fatalf("expected error for bad integer")
	}
}
