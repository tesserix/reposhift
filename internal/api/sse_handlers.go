package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SSE Event types
const (
	EventTypeMigrationUpdate = "migration_update"
	EventTypeProjectUpdate   = "project_update"
	EventTypeWorkItemUpdate  = "workitem_update"
	EventTypeSystemUpdate    = "system_update"
)

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Type      string      `json:"type"`
	ID        string      `json:"id"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// SSEClient represents a connected SSE client
type SSEClient struct {
	ID      string
	Channel chan SSEEvent
	Done    chan bool
}

// SSEHub manages SSE clients and broadcasts events
type SSEHub struct {
	clients    map[string]*SSEClient
	register   chan *SSEClient
	unregister chan *SSEClient
	broadcast  chan SSEEvent
}

// NewSSEHub creates a new SSE hub
func NewSSEHub() *SSEHub {
	hub := &SSEHub{
		clients:    make(map[string]*SSEClient),
		register:   make(chan *SSEClient),
		unregister: make(chan *SSEClient),
		broadcast:  make(chan SSEEvent, 100), // Buffered channel
	}
	go hub.run()
	return hub
}

// run handles client registration/unregistration and broadcasting
func (h *SSEHub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client.ID] = client
			log.Log.Info("SSE client connected", "clientID", client.ID, "total", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Channel)
				log.Log.Info("SSE client disconnected", "clientID", client.ID, "total", len(h.clients))
			}

		case event := <-h.broadcast:
			for _, client := range h.clients {
				select {
				case client.Channel <- event:
				default:
					// Client channel is full, skip this event for this client
					log.Log.V(1).Info("SSE client channel full, skipping event", "clientID", client.ID)
				}
			}
		}
	}
}

// BroadcastEvent sends an event to all connected clients
func (h *SSEHub) BroadcastEvent(eventType, id string, data interface{}) {
	event := SSEEvent{
		Type:      eventType,
		ID:        id,
		Data:      data,
		Timestamp: time.Now(),
	}
	h.broadcast <- event
}

// handleSSE handles Server-Sent Events connections
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	logger := log.FromContext(r.Context())

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create new SSE client
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())
	client := &SSEClient{
		ID:      clientID,
		Channel: make(chan SSEEvent, 10),
		Done:    make(chan bool),
	}

	// Register client
	s.sseHub.register <- client

	// Ensure client is unregistered on disconnect
	defer func() {
		s.sseHub.unregister <- client
	}()

	// Send initial connection event
	initialEvent := SSEEvent{
		Type:      EventTypeSystemUpdate,
		ID:        "connection",
		Data:      map[string]string{"status": "connected", "clientID": clientID},
		Timestamp: time.Now(),
	}
	s.sendSSEEvent(w, initialEvent)

	// Flush to ensure connection is established
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Context for timeout
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Send heartbeat every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("SSE client context done", "clientID", clientID)
			return

		case <-client.Done:
			logger.Info("SSE client done signal received", "clientID", clientID)
			return

		case event := <-client.Channel:
			if err := s.sendSSEEvent(w, event); err != nil {
				logger.Error(err, "Failed to send SSE event", "clientID", clientID)
				return
			}

		case <-ticker.C:
			// Send heartbeat
			heartbeat := SSEEvent{
				Type:      EventTypeSystemUpdate,
				ID:        "heartbeat",
				Data:      map[string]string{"status": "alive"},
				Timestamp: time.Now(),
			}
			if err := s.sendSSEEvent(w, heartbeat); err != nil {
				logger.Error(err, "Failed to send heartbeat", "clientID", clientID)
				return
			}
		}
	}
}

// sendSSEEvent writes an SSE event to the response writer
func (s *Server) sendSSEEvent(w http.ResponseWriter, event SSEEvent) error {
	// Marshal event data to JSON
	dataJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Write SSE format: "event: type\ndata: json\nid: id\n\n"
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\nid: %s\n\n", event.Type, dataJSON, event.ID)
	if err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	// Flush immediately
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
}

// Helper functions to broadcast specific event types

// BroadcastMigrationUpdate sends a migration progress update to all SSE clients
func (s *Server) BroadcastMigrationUpdate(migrationID string, data interface{}) {
	if s.sseHub != nil {
		s.sseHub.BroadcastEvent(EventTypeMigrationUpdate, migrationID, data)
	}
}

// BroadcastProjectUpdate sends a GitHub project update to all SSE clients
func (s *Server) BroadcastProjectUpdate(projectID string, data interface{}) {
	if s.sseHub != nil {
		s.sseHub.BroadcastEvent(EventTypeProjectUpdate, projectID, data)
	}
}

// BroadcastWorkItemUpdate sends a work item migration update to all SSE clients
func (s *Server) BroadcastWorkItemUpdate(migrationID string, data interface{}) {
	if s.sseHub != nil {
		s.sseHub.BroadcastEvent(EventTypeWorkItemUpdate, migrationID, data)
	}
}
