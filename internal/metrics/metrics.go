package metrics

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/kstaniek/go-ampio-server/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus counters
var (
	SerialRxFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "serial_rx_frames_total",
		Help: "Total CAN frames decoded from the serial link.",
	})
	SocketCANRxFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "socketcan_rx_frames_total",
		Help: "Total CAN frames read from the SocketCAN interface.",
	})
	SerialTxFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "serial_tx_frames_total",
		Help: "Total CAN frames written to the serial link.",
	})
	SocketCANTxFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "socketcan_tx_frames_total",
		Help: "Total CAN frames written to the SocketCAN interface.",
	})
	TCPRxFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tcp_rx_frames_total",
		Help: "Total CAN frames received from TCP clients.",
	})
	TCPTxFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "tcp_tx_frames_total",
		Help: "Total CAN frames sent to TCP clients.",
	})
	HubDroppedFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hub_dropped_frames_total",
		Help: "Total CAN frames dropped by hub due to slow clients.",
	})
	HubKickedClients = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hub_kicked_clients_total",
		Help: "Total clients disconnected due to backpressure kick policy.",
	})
	HubRejectedClients = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hub_rejected_clients_total",
		Help: "Total client connection attempts rejected (e.g., max-clients).",
	})
	HubActiveClients = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hub_active_clients",
		Help: "Current number of active connected clients.",
	})
	HubBroadcastFanout = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hub_broadcast_fanout",
		Help: "Number of clients targeted in the most recent broadcast.",
	})
	HubQueueDepthMax = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hub_queue_depth_max",
		Help: "Observed max queued frames among clients since last sample window.",
	})
	HubQueueDepthAvg = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hub_queue_depth_avg",
		Help: "Approximate average queued frames per client in last sample.",
	})
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "build_info",
		Help: "Build metadata (value is always 1).",
	}, []string{"version", "commit", "date"})
	Errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "errors_total",
		Help: "Error counters by subsystem.",
	}, []string{"where"})
	MalformedFrames = promauto.NewCounter(prometheus.CounterOpts{
		Name: "malformed_frames_total",
		Help: "Total rejected malformed frames (protocol violations, invalid length, truncated).",
	})
	readinessMu sync.RWMutex
	readinessFn func() bool
)

// Error label constants (stable label values to bound cardinality)
const (
	ErrTCPRead        = "tcp_read"
	ErrTCPWrite       = "tcp_write"
	ErrHandshake      = "handshake"
	ErrSerialWrite    = "serial_write"
	ErrSerialOverflow = "serial_tx_overflow"
	ErrSocketCANWrite = "socketcan_write"
	ErrSocketCANOver  = "socketcan_tx_overflow"
	ErrSerialRead     = "serial_read"
	ErrSocketCANRead  = "socketcan_read"
)

// StartHTTP serves Prometheus metrics at /metrics on the given mux.
// If mux is nil, a default mux is created and registered.
func StartHTTP(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready\n"))
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		logging.L().Info("metrics_listen", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.L().Error("metrics_http_error", "error", err)
		}
	}()
	return srv
}

// Local mirrored counters for easy logging (avoid Prometheus scraping in-process)
var (
	localSerialRx    uint64
	localSerialTx    uint64
	localSocketCANTx uint64
	localSocketCANRx uint64
	localTCPRx       uint64
	localTCPTx       uint64
	localHubDrop     uint64
	localHubKick     uint64
	localHubReject   uint64
	localErrors      uint64
	localHubClients  uint64
	localFanout      uint64
	localMalformed   uint64
	localQDMax       uint64
	localQDAvg       uint64
)

// Snapshot is a cheap copy of local counters.
type Snapshot struct {
	SerialRx      uint64
	SocketCANRx   uint64
	SerialTx      uint64
	SocketCANTx   uint64
	TCPRx         uint64
	TCPTx         uint64
	HubDrops      uint64
	HubKicks      uint64
	HubRejects    uint64
	Errors        uint64 // sum across error labels
	HubClients    uint64
	Fanout        uint64
	Malformed     uint64
	QueueDepthMax uint64
	QueueDepthAvg uint64
}

func Snap() Snapshot {
	return Snapshot{
		SerialRx:      atomic.LoadUint64(&localSerialRx),
		SocketCANRx:   atomic.LoadUint64(&localSocketCANRx),
		SerialTx:      atomic.LoadUint64(&localSerialTx),
		SocketCANTx:   atomic.LoadUint64(&localSocketCANTx),
		TCPRx:         atomic.LoadUint64(&localTCPRx),
		TCPTx:         atomic.LoadUint64(&localTCPTx),
		HubDrops:      atomic.LoadUint64(&localHubDrop),
		HubKicks:      atomic.LoadUint64(&localHubKick),
		HubRejects:    atomic.LoadUint64(&localHubReject),
		Errors:        atomic.LoadUint64(&localErrors),
		HubClients:    atomic.LoadUint64(&localHubClients),
		Fanout:        atomic.LoadUint64(&localFanout),
		Malformed:     atomic.LoadUint64(&localMalformed),
		QueueDepthMax: atomic.LoadUint64(&localQDMax),
		QueueDepthAvg: atomic.LoadUint64(&localQDAvg),
	}
}

// Wrapper helpers to keep call sites simple.
func IncSerialRx() {
	SerialRxFrames.Inc()
	atomic.AddUint64(&localSerialRx, 1)
}

// IncSocketCANRx increments SocketCAN receive counters.
func IncSocketCANRx() {
	SocketCANRxFrames.Inc()
	atomic.AddUint64(&localSocketCANRx, 1)
}

func IncSerialTx() {
	SerialTxFrames.Inc()
	atomic.AddUint64(&localSerialTx, 1)
}

// IncSocketCANTx increments SocketCAN transmit counters.
func IncSocketCANTx() {
	SocketCANTxFrames.Inc()
	atomic.AddUint64(&localSocketCANTx, 1)
}

func IncTCPRx() {
	TCPRxFrames.Inc()
	atomic.AddUint64(&localTCPRx, 1)
}

func AddTCPTx(n int) {
	TCPTxFrames.Add(float64(n))
	atomic.AddUint64(&localTCPTx, uint64(n))
}

func IncHubDrop() {
	HubDroppedFrames.Inc()
	atomic.AddUint64(&localHubDrop, 1)
}

func IncHubKick() {
	HubKickedClients.Inc()
	atomic.AddUint64(&localHubKick, 1)
}

func IncHubReject() {
	HubRejectedClients.Inc()
	atomic.AddUint64(&localHubReject, 1)
}

func SetHubClients(n int) {
	HubActiveClients.Set(float64(n))
	atomic.StoreUint64(&localHubClients, uint64(n))
}

func SetBroadcastFanout(n int) {
	HubBroadcastFanout.Set(float64(n))
	atomic.StoreUint64(&localFanout, uint64(n))
}

func IncError(label string) {
	Errors.WithLabelValues(label).Inc()
	atomic.AddUint64(&localErrors, 1)
}

func IncMalformed() {
	MalformedFrames.Inc()
	atomic.AddUint64(&localMalformed, 1)
}

// SetQueueDepth records a snapshot of max and avg queue depth.
func SetQueueDepth(max, avg int) {
	HubQueueDepthMax.Set(float64(max))
	HubQueueDepthAvg.Set(float64(avg))
	atomic.StoreUint64(&localQDMax, uint64(max))
	atomic.StoreUint64(&localQDAvg, uint64(avg))
}

// InitBuildInfo sets the build info gauge (should be called once at startup).
func InitBuildInfo(version, commit, date string) {
	BuildInfo.WithLabelValues(version, commit, date).Set(1)
	// Pre-register common error label series so first error does not log a registration latency.
	for _, lbl := range []string{
		ErrTCPRead, ErrTCPWrite, ErrHandshake,
		ErrSerialWrite, ErrSerialOverflow, ErrSerialRead,
		ErrSocketCANWrite, ErrSocketCANOver, ErrSocketCANRead,
	} {
		Errors.WithLabelValues(lbl).Add(0)
	}
}

// SetReadinessFunc registers a function used by /ready and IsReady.
func SetReadinessFunc(fn func() bool) { readinessMu.Lock(); readinessFn = fn; readinessMu.Unlock() }

// IsReady invokes the registered readiness function if present.
func IsReady() bool {
	readinessMu.RLock()
	fn := readinessFn
	readinessMu.RUnlock()
	if fn == nil { // if not set yet, treat as ready so metrics endpoint doesn't flap
		return true
	}
	return fn()
}

// Ready is a concise alias used at call sites.
func Ready() bool { return IsReady() }
