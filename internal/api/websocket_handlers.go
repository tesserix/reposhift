package api

import (
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tesserix/reposhift/internal/websocket"
)

// WebSocket handlers

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Log.Error(err, "Failed to upgrade WebSocket connection")
		return
	}

	client := websocket.NewClient(s.websocketManager, conn)
	s.websocketManager.Register(client)

	// Start client goroutines
	go client.WritePump()
	go client.ReadPump()
}

// WebSocket message types
type WebSocketMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp string      `json:"timestamp"`
}

// Send migration progress update via WebSocket
func (s *Server) sendMigrationUpdate(migrationID string, data interface{}) {
	s.websocketManager.BroadcastUpdate("migration_progress", "migration", migrationID, data)
}

// Send discovery update via WebSocket
func (s *Server) sendDiscoveryUpdate(discoveryID string, data interface{}) {
	s.websocketManager.BroadcastUpdate("discovery_progress", "discovery", discoveryID, data)
}

// Send pipeline conversion update via WebSocket
func (s *Server) sendPipelineUpdate(pipelineID string, data interface{}) {
	s.websocketManager.BroadcastUpdate("pipeline_progress", "pipeline", pipelineID, data)
}
