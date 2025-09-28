package main

import (
	"testing"
	"time"
)

func TestConfigValidate_OK(t *testing.T) {
	c := &appConfig{
		serialDev:    "/dev/null",
		baud:         115200,
		listenAddr:   ":20000",
		serialReadTO: 10 * time.Millisecond,
		logFormat:    "text",
		logLevel:     "info",
		hubBuffer:    8,
		hubPolicy:    "drop",
		backend:      "serial",
		canIf:        "can0",
		maxClients:   0,
		handshakeTO:  time.Second,
		clientReadTO: time.Second,
	}
	if err := c.validate(); err != nil {
		t.Fatalf("expected ok got %v", err)
	}
}

func TestConfigValidate_Errors(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*appConfig)
	}{
		{"badFormat", func(c *appConfig) { c.logFormat = "xx" }},
		{"badLevel", func(c *appConfig) { c.logLevel = "nope" }},
		{"badBackend", func(c *appConfig) { c.backend = "x" }},
		{"badPolicy", func(c *appConfig) { c.hubPolicy = "x" }},
		{"badHubBuf", func(c *appConfig) { c.hubBuffer = 0 }},
		{"badBaud", func(c *appConfig) { c.baud = 0 }},
		{"badSerialTO", func(c *appConfig) { c.serialReadTO = 0 }},
		{"badHandshakeTO", func(c *appConfig) { c.handshakeTO = 0 }},
		{"badClientReadTO", func(c *appConfig) { c.clientReadTO = 0 }},
		{"badMaxClients", func(c *appConfig) { c.maxClients = -1 }},
	}
	for _, tc := range tests {
		base := &appConfig{
			serialDev: "/dev/null", baud: 115200, listenAddr: ":20000", serialReadTO: 10 * time.Millisecond,
			logFormat: "text", logLevel: "info", hubBuffer: 8, hubPolicy: "drop", backend: "serial", canIf: "can0",
			maxClients: 0, handshakeTO: time.Second, clientReadTO: time.Second,
		}
		tc.mod(base)
		if err := base.validate(); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}
