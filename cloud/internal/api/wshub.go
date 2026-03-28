package api

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// ═══════════════════════════════════════════════════════════════════════════════
// WebSocket Hub — tracks all connected Qubes
// ═══════════════════════════════════════════════════════════════════════════════
//
// Architecture:
//   Cloud API (:8080/ws) ←WebSocket→ conf-agent (on each Qube)
//
// One Hub per cloud-api process. Each Qube connects as a WSClient.
// The hub handles:
//   - Register/unregister clients
//   - Send messages to specific Qubes (by qube_id)
//   - Broadcast to all connected Qubes (rare — used for global announcements)
//   - Track which Qubes are currently connected (ws_connected flag)

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type      string `json:"type"`       // "config_push", "command", "ack", "heartbeat", "live_telemetry", "error"
	QubeID    string `json:"qube_id"`    // Source or target Qube
	ID        string `json:"id"`         // Message ID (for delivery tracking)
	Payload   any    `json:"payload"`    // Type-specific data
	Timestamp int64  `json:"timestamp"`  // Unix milliseconds
}

// NewWSMessage creates a message with auto-generated timestamp.
func NewWSMessage(msgType, qubeID string, payload any) WSMessage {
	return WSMessage{
		Type:      msgType,
		QubeID:    qubeID,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
}

// WSHub manages all WebSocket connections.
type WSHub struct {
	mu      sync.RWMutex
	clients map[string]*WSClient // qube_id → client

	// Callbacks — set by the WebSocket handler to update DB state
	OnConnect    func(qubeID string)
	OnDisconnect func(qubeID string)
}

// NewWSHub creates a new hub instance.
func NewWSHub() *WSHub {
	return &WSHub{
		clients: make(map[string]*WSClient),
	}
}

// Register adds a client to the hub.
// If a client with the same qube_id already exists, the old one is closed.
func (h *WSHub) Register(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close existing connection for this qube (reconnection scenario)
	if old, exists := h.clients[client.QubeID]; exists {
		log.Printf("[ws-hub] replacing existing connection for %s", client.QubeID)
		old.Close()
	}

	h.clients[client.QubeID] = client
	log.Printf("[ws-hub] registered %s (total: %d)", client.QubeID, len(h.clients))

	if h.OnConnect != nil {
		go h.OnConnect(client.QubeID)
	}
}

// Unregister removes a client from the hub.
func (h *WSHub) Unregister(qubeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.clients[qubeID]; exists {
		delete(h.clients, qubeID)
		log.Printf("[ws-hub] unregistered %s (total: %d)", qubeID, len(h.clients))

		if h.OnDisconnect != nil {
			go h.OnDisconnect(qubeID)
		}
	}
}

// SendTo sends a message to a specific Qube. Returns false if not connected.
func (h *WSHub) SendTo(qubeID string, msg WSMessage) bool {
	h.mu.RLock()
	client, exists := h.clients[qubeID]
	h.mu.RUnlock()

	if !exists {
		return false
	}

	select {
	case client.send <- msg:
		return true
	default:
		// Send buffer full — client is slow, close it
		log.Printf("[ws-hub] send buffer full for %s, closing", qubeID)
		h.Unregister(qubeID)
		client.Close()
		return false
	}
}

// IsConnected checks if a Qube is currently connected via WebSocket.
func (h *WSHub) IsConnected(qubeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, exists := h.clients[qubeID]
	return exists
}

// ConnectedQubes returns a list of all connected qube IDs.
func (h *WSHub) ConnectedQubes() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// Broadcast sends a message to all connected Qubes.
func (h *WSHub) Broadcast(msg WSMessage) {
	h.mu.RLock()
	clients := make([]*WSClient, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- msg:
		default:
			// Skip slow clients
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// WSClient — wraps a single WebSocket connection
// ═══════════════════════════════════════════════════════════════════════════════

// WSClient represents a single connected Qube.
type WSClient struct {
	QubeID string
	OrgID  string
	Hub    *WSHub
	send   chan WSMessage
	done   chan struct{}
	once   sync.Once
}

// NewWSClient creates a client (connection handling is in websocket.go).
func NewWSClient(qubeID, orgID string, hub *WSHub) *WSClient {
	return &WSClient{
		QubeID: qubeID,
		OrgID:  orgID,
		Hub:    hub,
		send:   make(chan WSMessage, 64),
		done:   make(chan struct{}),
	}
}

// Close signals the client to shut down.
func (c *WSClient) Close() {
	c.once.Do(func() {
		close(c.done)
	})
}

// IsClosed returns true if the client has been closed.
func (c *WSClient) IsClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// ═══════════════════════════════════════════════════════════════════════════════
// DashboardHub — tracks dashboard WebSocket subscribers for live telemetry
// ═══════════════════════════════════════════════════════════════════════════════
//
// Dashboard clients (browser users) subscribe to specific Qubes to receive
// live telemetry in real-time. Each client is authenticated via JWT and scoped
// to their org — they can only subscribe to Qubes they own.

// DashboardClient represents a single browser WebSocket connection.
type DashboardClient struct {
	ID     string          // Unique connection ID
	OrgID  string          // Organisation scope (from JWT)
	Qubes  map[string]bool // Subscribed qube_id set
	send   chan WSMessage
	done   chan struct{}
	once   sync.Once
}

// NewDashboardClient creates a dashboard subscriber.
func NewDashboardClient(id, orgID string) *DashboardClient {
	return &DashboardClient{
		ID:    id,
		OrgID: orgID,
		Qubes: make(map[string]bool),
		send:  make(chan WSMessage, 64),
		done:  make(chan struct{}),
	}
}

// Close signals the dashboard client to shut down.
func (c *DashboardClient) Close() {
	c.once.Do(func() {
		close(c.done)
	})
}

// DashboardHub manages dashboard WebSocket subscribers.
type DashboardHub struct {
	mu      sync.RWMutex
	clients map[string]*DashboardClient // connection ID → client
}

// NewDashboardHub creates a new dashboard hub.
func NewDashboardHub() *DashboardHub {
	return &DashboardHub{
		clients: make(map[string]*DashboardClient),
	}
}

// Register adds a dashboard client.
func (h *DashboardHub) Register(client *DashboardClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client.ID] = client
	log.Printf("[dash-hub] registered %s (org=%s, total=%d)", client.ID, client.OrgID, len(h.clients))
}

// Unregister removes a dashboard client.
func (h *DashboardHub) Unregister(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, exists := h.clients[id]; exists {
		c.Close()
		delete(h.clients, id)
		log.Printf("[dash-hub] unregistered %s (total=%d)", id, len(h.clients))
	}
}

// Subscribe adds a Qube to a client's subscription list.
func (h *DashboardHub) Subscribe(clientID, qubeID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c, exists := h.clients[clientID]; exists {
		c.Qubes[qubeID] = true
	}
}

// Unsubscribe removes a Qube from a client's subscription list.
func (h *DashboardHub) Unsubscribe(clientID, qubeID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if c, exists := h.clients[clientID]; exists {
		delete(c.Qubes, qubeID)
	}
}

// RelayToSubscribers sends a message to all dashboard clients subscribed to
// the given qubeID and belonging to the given orgID.
func (h *DashboardHub) RelayToSubscribers(orgID, qubeID string, msg WSMessage) int {
	h.mu.RLock()
	var targets []*DashboardClient
	for _, c := range h.clients {
		if c.OrgID == orgID && c.Qubes[qubeID] {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	sent := 0
	for _, c := range targets {
		select {
		case c.send <- msg:
			sent++
		default:
			// Skip slow dashboard clients
		}
	}
	return sent
}

// ConnectedCount returns the number of dashboard connections.
func (h *DashboardHub) ConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// MarshalMessage serializes a WSMessage to JSON bytes.
func MarshalMessage(msg WSMessage) ([]byte, error) {
	return json.Marshal(msg)
}

// UnmarshalMessage deserializes JSON bytes to a WSMessage.
func UnmarshalMessage(data []byte) (WSMessage, error) {
	var msg WSMessage
	err := json.Unmarshal(data, &msg)
	return msg, err
}
