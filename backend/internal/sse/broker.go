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

const (
	clientBufferSize  = 32
	heartbeatInterval = 15 * time.Second
)

// Broker coordinates connected SSE clients and broadcasts realtime notifications.
//
// Concurrency model: the listen goroutine is the ONLY goroutine that sends on or
// closes a client channel, so a channel is closed exactly once and never sent on
// after close. Sends are non-blocking; a client whose buffer is full is evicted
// (its channel closed) so its HTTP handler returns and the browser's EventSource
// reconnects and refetches state, instead of silently missing a MED_LOGGED event
// and showing a dose as "not given".
type Broker struct {
	clients        map[chan string]struct{}
	newClients     chan chan string
	closingClients chan chan string
	events         chan string
	countReq       chan chan int
	done           chan struct{}
	closeOnce      sync.Once
	heartbeat      time.Duration
}

func InitBroker() *Broker {
	return newBroker(heartbeatInterval)
}

func newBroker(heartbeat time.Duration) *Broker {
	b := &Broker{
		clients:        make(map[chan string]struct{}),
		newClients:     make(chan chan string),
		closingClients: make(chan chan string),
		events:         make(chan string, 256),
		countReq:       make(chan chan int),
		done:           make(chan struct{}),
		heartbeat:      heartbeat,
	}
	go b.listen()
	return b
}

// Close stops the broker goroutine and disconnects every client.
func (b *Broker) Close() {
	b.closeOnce.Do(func() { close(b.done) })
}

func (b *Broker) listen() {
	ticker := time.NewTicker(b.heartbeat)
	defer ticker.Stop()

	fanout := func(msg string) {
		for client := range b.clients {
			select {
			case client <- msg:
			default:
				// Slow consumer: never block the broadcaster. Evict it so it resyncs.
				delete(b.clients, client)
				close(client)
				log.Printf("[SSE] Evicted slow client. Total active listeners: %d", len(b.clients))
			}
		}
	}

	for {
		select {
		case <-b.done:
			for client := range b.clients {
				delete(b.clients, client)
				close(client)
			}
			return

		case client := <-b.newClients:
			b.clients[client] = struct{}{}
			log.Printf("[SSE] Client connected. Total active listeners: %d", len(b.clients))

		case client := <-b.closingClients:
			if _, ok := b.clients[client]; ok {
				delete(b.clients, client)
				close(client)
				log.Printf("[SSE] Client disconnected. Total active listeners: %d", len(b.clients))
			}

		case reply := <-b.countReq:
			reply <- len(b.clients)

		case event := <-b.events:
			fanout(event)

		case <-ticker.C:
			// SSE comment line keeps Cloudflare / mobile proxies from idling out the stream
			fanout(": ping\n\n")
		}
	}
}

// subscribe registers a new client channel. Returns nil if the broker is closed.
func (b *Broker) subscribe(bufSize int) chan string {
	ch := make(chan string, bufSize)
	select {
	case b.newClients <- ch:
		return ch
	case <-b.done:
		return nil
	}
}

// unsubscribe removes a client. Safe to call after eviction or broker Close.
func (b *Broker) unsubscribe(ch chan string) {
	select {
	case b.closingClients <- ch:
	case <-b.done:
	}
}

// ClientCount returns the number of connected clients (0 once closed).
func (b *Broker) ClientCount() int {
	reply := make(chan int, 1)
	select {
	case b.countReq <- reply:
		return <-reply
	case <-b.done:
		return 0
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
	select {
	case b.events <- data:
	case <-b.done:
	}
}

// BroadcastAlert is Broadcast for events that created an alert (issue #12): the
// message id and the SSE "id:" field are the alert id, so a client can resume
// with Last-Event-ID and acknowledge exactly that alert.
func (b *Broker) BroadcastAlert(alertID, eventType string, actor models.ActorInfo, payload interface{}) {
	msg := models.SSEMessage{
		ID:        alertID,
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
	data := fmt.Sprintf("id: %s\nevent: message\ndata: %s\n\n", alertID, string(bytes))
	select {
	case b.events <- data:
	case <-b.done:
	}
}

// ServeHTTP handles the SSE streaming connection endpoint.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// The server has WriteTimeout=0 for SSE; also clear any per-request deadline
	// that a wrapper may have set (ignored if unsupported).
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	messageChan := b.subscribe(clientBufferSize)
	if messageChan == nil {
		http.Error(w, "Event stream unavailable", http.StatusServiceUnavailable)
		return
	}
	defer b.unsubscribe(messageChan)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Initial handshake; retry hint tells EventSource to reconnect after 3s
	initEvent := fmt.Sprintf("retry: 3000\nevent: connected\ndata: {\"status\":\"connected\",\"time\":\"%s\"}\n\n", time.Now().UTC().Format(time.RFC3339))
	if _, err := w.Write([]byte(initEvent)); err != nil {
		return
	}
	flusher.Flush()

	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return
		case msg, ok := <-messageChan:
			if !ok {
				return // evicted or broker closed
			}
			if _, err := w.Write([]byte(msg)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
