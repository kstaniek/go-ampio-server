package hub

import (
	"sync"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/logging"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

type BackpressurePolicy int

const (
	PolicyDrop BackpressurePolicy = iota
	PolicyKick
)

type Client struct {
	Out       chan can.Frame
	Closed    chan struct{}
	closeOnce sync.Once
}

// Close signals the client is closed (idempotent).
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.Closed)
	})
}

type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]struct{}
	OutBufSize int
	Policy     BackpressurePolicy
}

// New creates a Hub with default settings.
func New() *Hub { return &Hub{clients: make(map[*Client]struct{})} }

// Add registers a client with the hub.
func (h *Hub) Add(c *Client) {
	h.mu.Lock()
	prev := len(h.clients)
	h.clients[c] = struct{}{}
	cur := len(h.clients)
	h.mu.Unlock()
	if prev == 0 && cur == 1 {
		logging.L().Info("clients_first_connected")
	}
}

// Remove unregisters a client and updates metrics; safe to call multiple times.
func (h *Hub) Remove(c *Client) {
	h.mu.Lock()
	_, existed := h.clients[c]
	if existed {
		delete(h.clients, c)
	}
	cur := len(h.clients)
	h.mu.Unlock()
	select {
	case <-c.Closed:
	default:
		c.Close()
	}
	metrics.SetHubClients(cur)
	if existed && cur == 0 {
		logging.L().Info("clients_last_disconnected")
	}
}

// Broadcast sends a frame to all connected clients honoring the backpressure policy.
func (h *Hub) Broadcast(fr can.Frame) {
	// Reuse Snapshot to avoid duplicating slice copy logic.
	clients := h.Snapshot()
	metrics.SetBroadcastFanout(len(clients))
	metrics.SetHubClients(len(clients))
	// queue depth sampling
	if len(clients) > 0 {
		max := 0
		sum := 0
		for _, c := range clients {
			l := len(c.Out)
			if l > max {
				max = l
			}
			sum += l
		}
		metrics.SetQueueDepth(max, sum/len(clients))
	}
	for _, c := range clients {
		select {
		case c.Out <- fr:
		default:
			if h.Policy == PolicyKick {
				metrics.IncHubKick()
				c.Close() // signal writer to exit; server will Remove on disconnect
			} else {
				metrics.IncHubDrop()
			}
		}
	}
}

// Snapshot returns a slice copy of current clients (read-only use).
func (h *Hub) Snapshot() []*Client {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	return clients
}

// Count returns the number of active clients.
func (h *Hub) Count() int { h.mu.RLock(); n := len(h.clients); h.mu.RUnlock(); return n }
