package server

import (
	"bytes"
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// WebSocketHub maintains the set of active clients and broadcasts messages to the clients.
type WebSocketHub struct {
	// Registered clients.
	clients map[*WebSocketClient]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *WebSocketClient

	// Unregister requests from clients.
	unregister chan *WebSocketClient
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		broadcast:  make(chan []byte),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
		clients:    make(map[*WebSocketClient]bool),
	}
}

// Run starts the WebSocket hub
func (h *WebSocketHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("WebSocket client connected. Total clients: %d", len(h.clients))

			// Send welcome message
			welcome := map[string]interface{}{
				"type":      "connection",
				"message":   "Connected to migration operator",
				"timestamp": time.Now(),
			}
			if data, err := json.Marshal(welcome); err == nil {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("WebSocket client disconnected. Total clients: %d", len(h.clients))
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// BroadcastUpdate sends an update to all connected clients
func (h *WebSocketHub) BroadcastUpdate(updateType, resourceType, resourceName string, data interface{}) {
	message := map[string]interface{}{
		"type":         updateType,
		"resourceType": resourceType,
		"resourceName": resourceName,
		"data":         data,
		"timestamp":    time.Now(),
	}

	if msgData, err := json.Marshal(message); err == nil {
		h.broadcast <- msgData
	}
}

// WebSocketClient is a middleman between the websocket connection and the hub.
type WebSocketClient struct {
	hub *WebSocketHub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
}

// readPump pumps messages from the websocket connection to the hub.
func (c *WebSocketClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))

		// Handle incoming messages (could be used for client commands)
		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err == nil {
			// Process client message if needed
			log.Printf("Received WebSocket message: %v", msg)
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
