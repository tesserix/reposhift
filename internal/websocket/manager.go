package websocket

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Manager handles WebSocket connections and broadcasting
type Manager struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from clients
	broadcast chan []byte

	// Register requests from clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Mutex for thread-safe operations
	mutex sync.RWMutex

	// Control channel for stopping the manager
	stop chan struct{}

	// Whether the manager is running
	running bool
}

// NewManager creates a new WebSocket manager
func NewManager() *Manager {
	return &Manager{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		stop:       make(chan struct{}),
		running:    false,
	}
}

// Start starts the WebSocket manager
func (m *Manager) Start() {
	m.mutex.Lock()
	if m.running {
		m.mutex.Unlock()
		return
	}
	m.running = true
	m.mutex.Unlock()

	log.Println("WebSocket Manager started")

	go m.run()
}

// Stop stops the WebSocket manager
func (m *Manager) Stop() {
	m.mutex.Lock()
	if !m.running {
		m.mutex.Unlock()
		return
	}
	m.running = false
	m.mutex.Unlock()

	close(m.stop)
	log.Println("WebSocket Manager stopped")
}

// run handles the main event loop for the WebSocket manager
func (m *Manager) run() {
	for {
		select {
		case client := <-m.register:
			m.mutex.Lock()
			m.clients[client] = true
			m.mutex.Unlock()

			log.Printf("WebSocket client connected. Total clients: %d", len(m.clients))

			// Send welcome message
			welcome := map[string]interface{}{
				"type":      "connection",
				"message":   "Connected to migration operator",
				"timestamp": time.Now(),
			}
			if data, err := json.Marshal(welcome); err == nil {
				select {
				case client.send <- data:
					// Message sent successfully
				default:
					// Client's send channel is full, unregister it
					m.unregisterClient(client)
				}
			}

		case client := <-m.unregister:
			m.unregisterClient(client)

		case message := <-m.broadcast:
			m.mutex.RLock()
			for client := range m.clients {
				select {
				case client.send <- message:
					// Message sent successfully
				default:
					// Client's send channel is full, unregister it
					m.unregisterClient(client)
				}
			}
			m.mutex.RUnlock()

		case <-m.stop:
			// Clean up all clients
			m.mutex.Lock()
			for client := range m.clients {
				close(client.send)
				client.conn.Close()
			}
			m.clients = make(map[*Client]bool)
			m.mutex.Unlock()
			return
		}
	}
}

// unregisterClient removes a client from the manager
func (m *Manager) unregisterClient(client *Client) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.clients[client]; ok {
		delete(m.clients, client)
		close(client.send)
		client.conn.Close()
		log.Printf("WebSocket client disconnected. Total clients: %d", len(m.clients))
	}
}

// Register registers a new client
func (m *Manager) Register(client *Client) {
	m.register <- client
}

// Unregister unregisters a client
func (m *Manager) Unregister(client *Client) {
	m.unregister <- client
}

// BroadcastUpdate sends an update to all connected WebSocket clients
func (m *Manager) BroadcastUpdate(updateType, resourceType, resourceName string, data interface{}) {
	message := map[string]interface{}{
		"type":         updateType,
		"resourceType": resourceType,
		"resourceName": resourceName,
		"data":         data,
		"timestamp":    time.Now(),
	}

	if msgData, err := json.Marshal(message); err == nil {
		select {
		case m.broadcast <- msgData:
			// Message queued for broadcast
		default:
			// Broadcast channel is full, log the issue
			log.Printf("WebSocket broadcast channel full, dropping message")
		}
	}
}

// HandleWebSocket handles a new WebSocket connection
func (m *Manager) HandleWebSocket(conn *websocket.Conn) {
	client := NewClient(m, conn)
	m.Register(client)

	// Start client goroutines
	go client.WritePump()
	go client.ReadPump()
}

// GetClientCount returns the number of connected clients
func (m *Manager) GetClientCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.clients)
}
