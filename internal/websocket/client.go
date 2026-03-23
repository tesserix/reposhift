package websocket

import (
	"bytes"
	"encoding/json"
	"log"
	"sync"
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
	maxMessageSize = 4096
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client represents a WebSocket client
type Client struct {
	manager   *Manager
	conn      *websocket.Conn
	send      chan []byte
	lastPing  time.Time
	userInfo  map[string]interface{}
	userMutex sync.RWMutex
}

// NewClient creates a new WebSocket client
func NewClient(manager *Manager, conn *websocket.Conn) *Client {
	return &Client{
		manager:  manager,
		conn:     conn,
		send:     make(chan []byte, 256),
		lastPing: time.Now(),
		userInfo: make(map[string]interface{}),
	}
}

// ReadPump pumps messages from the websocket connection to the manager
func (c *Client) ReadPump() {
	defer func() {
		c.manager.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.lastPing = time.Now()
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
			c.handleMessage(msg)
		}
	}
}

// WritePump pumps messages from the manager to the websocket connection
func (c *Client) WritePump() {
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

// handleMessage handles incoming messages from the client
func (c *Client) handleMessage(msg map[string]interface{}) {
	msgType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "ping":
		// Respond with pong
		response := map[string]interface{}{
			"type":      "pong",
			"timestamp": time.Now(),
		}
		if data, err := json.Marshal(response); err == nil {
			select {
			case c.send <- data:
				// Message sent
			default:
				c.manager.Unregister(c)
			}
		}

	case "subscribe":
		// Handle subscription to specific events
		if events, ok := msg["events"].([]interface{}); ok {
			c.userMutex.Lock()
			c.userInfo["subscriptions"] = events
			c.userMutex.Unlock()

			response := map[string]interface{}{
				"type":      "subscribed",
				"events":    events,
				"timestamp": time.Now(),
			}
			if data, err := json.Marshal(response); err == nil {
				select {
				case c.send <- data:
					// Message sent
				default:
					c.manager.Unregister(c)
				}
			}
		}

	case "unsubscribe":
		// Handle unsubscription from specific events
		c.userMutex.Lock()
		c.userInfo["subscriptions"] = nil
		c.userMutex.Unlock()

		response := map[string]interface{}{
			"type":      "unsubscribed",
			"timestamp": time.Now(),
		}
		if data, err := json.Marshal(response); err == nil {
			select {
			case c.send <- data:
				// Message sent
			default:
				c.manager.Unregister(c)
			}
		}

	case "identify":
		// Handle client identification
		if identity, ok := msg["identity"].(map[string]interface{}); ok {
			c.userMutex.Lock()
			for k, v := range identity {
				c.userInfo[k] = v
			}
			c.userMutex.Unlock()

			response := map[string]interface{}{
				"type":      "identified",
				"timestamp": time.Now(),
			}
			if data, err := json.Marshal(response); err == nil {
				select {
				case c.send <- data:
					// Message sent
				default:
					c.manager.Unregister(c)
				}
			}
		}

	default:
		log.Printf("Unknown message type: %s", msgType)
	}
}

// IsSubscribed checks if the client is subscribed to a specific event type
func (c *Client) IsSubscribed(eventType string) bool {
	c.userMutex.RLock()
	defer c.userMutex.RUnlock()

	// If no subscriptions, assume subscribed to all
	subscriptions, ok := c.userInfo["subscriptions"]
	if !ok || subscriptions == nil {
		return true
	}

	// Check if event type is in subscriptions
	if events, ok := subscriptions.([]interface{}); ok {
		for _, event := range events {
			if eventStr, ok := event.(string); ok && eventStr == eventType {
				return true
			}
		}
	}

	return false
}

// SetUserInfo sets user information
func (c *Client) SetUserInfo(key string, value interface{}) {
	c.userMutex.Lock()
	defer c.userMutex.Unlock()
	c.userInfo[key] = value
}

// GetUserInfo gets user information
func (c *Client) GetUserInfo(key string) (interface{}, bool) {
	c.userMutex.RLock()
	defer c.userMutex.RUnlock()
	value, ok := c.userInfo[key]
	return value, ok
}
