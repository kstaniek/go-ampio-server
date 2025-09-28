package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type appConfig struct {
	serialDev       string
	baud            int
	listenAddr      string
	serialReadTO    time.Duration
	logFormat       string
	logLevel        string
	metricsAddr     string
	hubBuffer       int
	hubPolicy       string
	logMetricsEvery time.Duration
	backend         string
	canIf           string
	maxClients      int
	handshakeTO     time.Duration
	clientReadTO    time.Duration
	mdnsEnable      bool
	mdnsName        string
}

func parseFlags() (*appConfig, bool) {
	cfg := &appConfig{}
	serialDev := flag.String("serial", "/dev/ttyUSB0", "Serial device path")
	baud := flag.Int("baud", 115200, "Serial baud rate")
	listen := flag.String("listen", ":20000", "TCP listen address")
	serialReadTO := flag.Duration("serial-read-timeout", 50*time.Millisecond, "Serial read timeout")
	logFormat := flag.String("log-format", "text", "Log format: text|json")
	logLevel := flag.String("log-level", "info", "Log level: debug|info|warn|error")
	metricsAddr := flag.String("metrics-addr", "", "Metrics HTTP listen address (e.g., :9100); empty disables")
	hubBuf := flag.Int("hub-buffer", 512, "Per-client hub buffer (frames)")
	hubPolicy := flag.String("hub-policy", "drop", "Backpressure policy: drop|kick")
	logMetricsEvery := flag.Duration("log-metrics-interval", 0, "If >0, periodically log metrics counters (for non-Prometheus setups)")
	backend := flag.String("backend", "socketcan", "CAN backend: serial|socketcan (default socketcan)")
	canIf := flag.String("can-if", "can0", "SocketCAN interface (when --backend=socketcan)")
	maxClients := flag.Int("max-clients", 0, "Maximum simultaneous TCP clients (0 = unlimited)")
	handshakeTO := flag.Duration("handshake-timeout", 3*time.Second, "Client handshake timeout")
	clientReadTO := flag.Duration("client-read-timeout", 60*time.Second, "Per-connection read deadline")
	mdnsEnable := flag.Bool("mdns-enable", false, "Enable mDNS/Avahi advertisement (packaged systemd unit enables by default)")
	mdnsName := flag.String("mdns-name", "", "mDNS instance name (default can-server-<hostname>)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	// Track which flags were explicitly set to give them precedence over env.
	setFlags := map[string]struct{}{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = struct{}{} })
	cfg.serialDev = *serialDev
	cfg.baud = *baud
	cfg.listenAddr = *listen
	cfg.serialReadTO = *serialReadTO
	cfg.logFormat = *logFormat
	cfg.logLevel = *logLevel
	cfg.metricsAddr = *metricsAddr
	cfg.hubBuffer = *hubBuf
	cfg.hubPolicy = *hubPolicy
	cfg.logMetricsEvery = *logMetricsEvery
	cfg.backend = *backend
	cfg.canIf = *canIf
	cfg.maxClients = *maxClients
	cfg.handshakeTO = *handshakeTO
	cfg.clientReadTO = *clientReadTO
	cfg.mdnsEnable = *mdnsEnable
	cfg.mdnsName = *mdnsName

	if err := applyEnvOverrides(cfg, setFlags); err != nil {
		fmt.Printf("environment override error: %v\n", err)
		return nil, *showVersion
	}
	// service type & TXT records now fixed internally; only enable + name configurable
	if err := cfg.validate(); err != nil {
		fmt.Printf("configuration error: %v\n", err)
		return nil, *showVersion
	}
	return cfg, *showVersion
}

// validate performs basic semantic validation of the parsed configuration.
// It does not attempt to open devices or listeners – only checks values/ranges.
func (c *appConfig) validate() error {
	if c == nil {
		return errors.New("nil config")
	}
	switch c.logFormat {
	case "text", "json":
	default:
		return fmt.Errorf("invalid log-format: %s", c.logFormat)
	}
	switch c.logLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log-level: %s", c.logLevel)
	}
	switch c.backend {
	case "serial", "socketcan":
	default:
		return fmt.Errorf("invalid backend: %s", c.backend)
	}
	switch c.hubPolicy {
	case "drop", "kick":
	default:
		return fmt.Errorf("invalid hub-policy: %s", c.hubPolicy)
	}
	if c.hubBuffer <= 0 {
		return fmt.Errorf("hub-buffer must be > 0 (got %d)", c.hubBuffer)
	}
	if c.baud <= 0 {
		return fmt.Errorf("baud must be > 0 (got %d)", c.baud)
	}
	if c.serialReadTO <= 0 {
		return fmt.Errorf("serial-read-timeout must be > 0")
	}
	if c.handshakeTO <= 0 {
		return fmt.Errorf("handshake-timeout must be > 0")
	}
	if c.clientReadTO <= 0 {
		return fmt.Errorf("client-read-timeout must be > 0")
	}
	if c.maxClients < 0 {
		return fmt.Errorf("max-clients must be >= 0")
	}
	// No extra validation needed for mDNS besides enable flag.
	return nil
}

// applyEnvOverrides maps CAN_SERVER_* environment variables to config fields
// unless a corresponding flag was explicitly set. Boolean & numeric parsing is lax:
// empty values ignored. Duration accepts Go time.ParseDuration format.
func applyEnvOverrides(c *appConfig, set map[string]struct{}) error {
	// mapping: env var -> apply func
	// Only apply if NOT in set (flag wins).
	var firstErr error
	get := func(k string) (string, bool) { v, ok := os.LookupEnv(k); return strings.TrimSpace(v), ok }
	if _, ok := set["serial"]; !ok {
		if v, ok := get("CAN_SERVER_SERIAL"); ok && v != "" {
			c.serialDev = v
		}
	}
	if _, ok := set["baud"]; !ok {
		if v, ok := get("CAN_SERVER_BAUD"); ok && v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.baud = n
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_BAUD: %w", err)
			}
		}
	}
	if _, ok := set["listen"]; !ok {
		if v, ok := get("CAN_SERVER_LISTEN"); ok && v != "" {
			c.listenAddr = v
		}
	}
	if _, ok := set["serial-read-timeout"]; !ok {
		if v, ok := get("CAN_SERVER_SERIAL_READ_TIMEOUT"); ok && v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				c.serialReadTO = d
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_SERIAL_READ_TIMEOUT: %w", err)
			}
		}
	}
	if _, ok := set["log-format"]; !ok {
		if v, ok := get("CAN_SERVER_LOG_FORMAT"); ok && v != "" {
			c.logFormat = v
		}
	}
	if _, ok := set["log-level"]; !ok {
		if v, ok := get("CAN_SERVER_LOG_LEVEL"); ok && v != "" {
			c.logLevel = v
		}
	}
	if _, ok := set["metrics-addr"]; !ok {
		if v, ok := get("CAN_SERVER_METRICS"); ok {
			c.metricsAddr = v
		}
	}
	if _, ok := set["hub-buffer"]; !ok {
		if v, ok := get("CAN_SERVER_HUB_BUFFER"); ok && v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.hubBuffer = n
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_HUB_BUFFER: %w", err)
			}
		}
	}
	if _, ok := set["hub-policy"]; !ok {
		if v, ok := get("CAN_SERVER_HUB_POLICY"); ok && v != "" {
			c.hubPolicy = v
		}
	}
	if _, ok := set["backend"]; !ok {
		if v, ok := get("CAN_SERVER_BACKEND"); ok && v != "" {
			c.backend = v
		}
	}
	if _, ok := set["can-if"]; !ok {
		if v, ok := get("CAN_SERVER_IF"); ok && v != "" {
			c.canIf = v
		}
	}
	if _, ok := set["max-clients"]; !ok {
		if v, ok := get("CAN_SERVER_MAX_CLIENTS"); ok && v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				c.maxClients = n
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_MAX_CLIENTS: %w", err)
			}
		}
	}
	if _, ok := set["handshake-timeout"]; !ok {
		if v, ok := get("CAN_SERVER_HANDSHAKE_TIMEOUT"); ok && v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				c.handshakeTO = d
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_HANDSHAKE_TIMEOUT: %w", err)
			}
		}
	}
	if _, ok := set["client-read-timeout"]; !ok {
		if v, ok := get("CAN_SERVER_CLIENT_READ_TIMEOUT"); ok && v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				c.clientReadTO = d
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_CLIENT_READ_TIMEOUT: %w", err)
			}
		}
	}
	if _, ok := set["mdns-enable"]; !ok {
		if v, ok := get("CAN_SERVER_MDNS_ENABLE"); ok && v != "" {
			switch strings.ToLower(v) {
			case "1", "true", "yes", "on":
				c.mdnsEnable = true
			case "0", "false", "no", "off":
				c.mdnsEnable = false
			}
		}
	}
	if _, ok := set["mdns-name"]; !ok {
		if v, ok := get("CAN_SERVER_MDNS_NAME"); ok && v != "" {
			c.mdnsName = v
		}
	}
	if _, ok := set["log-metrics-interval"]; !ok {
		if v, ok := get("CAN_SERVER_LOG_METRICS_INTERVAL"); ok && v != "" {
			if d, err := time.ParseDuration(v); err == nil && d >= 0 {
				c.logMetricsEvery = d
			} else if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("invalid CAN_SERVER_LOG_METRICS_INTERVAL: %w", err)
			}
		}
	}
	return firstErr
}
