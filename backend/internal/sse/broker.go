package sse

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"birrcare/backend/internal/models"
	"github.com/google/uuid"
)

// Broker coordinates connected SSE clients and broadcasts realtime notifications.
type Broker struct {
	clients        map[chan string]bool
	newClients     chan chan string
	closingClients chan chan string
	events         chan string
	mu             sync.RWMutex
}

var GlobalBroker *Broker

func InitBroker() *Broker {
	b := &Broker{
		clients:        make(map[chan string]bool),
		newClients:     make(chan chan string),
		closingClients: make(chan chan string),
		events:         make(chan string, 100),
	}
	GlobalBroker = b
	go b.listen()
	return b
}

func (b *Broker) listen() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-b.newClients:
			b.mu.Lock()
			b.clients[client] = true
			b.mu.Unlock()
			log.Printf("[SSE] Client connected. Total active listeners: %d", len(b.clients))

		case client := <-b.closingClients:
			b.mu.Lock()
			if _, ok := b.clients[client]; ok {
				delete(b.clients, client)
				close(client)
			}
			b.mu.Unlock()
			log.Printf("[SSE] Client disconnected. Total active listeners: %d", len(b.clients))

		case event := <-b.events:
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client <- event:
				default:
					// Slow consumer, avoid blocking the broker
				}
			}
			b.mu.RUnlock()

		case <-ticker.C:
			// Send heartbeat comment to keep mobile SSE connections alive
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client <- ": ping\n\n":
				default:
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Broadcast sends a typed SSE message to all connected clients.
func (b *Broker) Broadcast(eventType string, actor models.ActorInfo, payload interface{}) {
	msg := models.SSEMessage{
		ID:        uuid.New().String(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Actor:     actor,
		Payload:   payload,
	}

	bytes, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[SSE] Error marshaling event: %v", err)
		return
	}

	data := fmt.Sprintf("event: message\ndata: %s\n\n", string(bytes))
	b.events <- data
}

// ServeHTTP handles the SSE streaming connection endpoint.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Channel for this client
	messageChan := make(chan string, 10)
	b.newClients <- messageChan

	defer func() {
		b.closingClients <- messageChan
	}()

	// Send initial connection handshake
	initEvent := fmt.Sprintf("event: connected\ndata: {\"status\":\"connected\",\"time\":\"%s\"}\n\n", time.Now().UTC().Format(time.RFC3339))
	w.Write([]byte(initEvent))
	flusher.Flush()

	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return
		case msg, ok := <-messageChan:
			if !ok {
				return
			}
			if _, err := w.Write([]byte(msg)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
