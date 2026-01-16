package events

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// EventType defines the type of event
type EventType string

const (
	TypeDecision EventType = "decision"
	TypeError    EventType = "error"
	TypeInfo     EventType = "info"
	TypeTrade    EventType = "trade"
)

// Event represents a notification to be sent to clients
type Event struct {
	Type      EventType   `json:"type"`
	TraderID  string      `json:"trader_id,omitempty"`
	Symbol    string      `json:"symbol,omitempty"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// Hub maintains the set of active clients and broadcasts messages to the clients.
type Hub struct {
	// Registered clients.
	clients map[chan []byte]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan chan []byte

	// Unregister requests from clients.
	unregister chan chan []byte

	// CORS allowed origins
	allowedOrigins []string

	mu sync.Mutex
}

func NewHub(allowedOrigins []string) *Hub {
	return &Hub{
		broadcast:      make(chan []byte),
		allowedOrigins: allowedOrigins,
		register:   make(chan chan []byte),
		unregister: make(chan chan []byte),
		clients:    make(map[chan []byte]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[EventHub] Client registered. Total clients: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client)
			}
			h.mu.Unlock()
			log.Printf("[EventHub] Client unregistered. Total clients: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client <- message:
				default:
					close(client)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast sends an event to all connected clients
func (h *Hub) Broadcast(evt Event) {
	bytes, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[EventHub] Failed to marshal event: %v", err)
		return
	}
	h.broadcast <- bytes
}

// ServeHTTP handles SSE connections
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// SECURITY: Set CORS only for allowed origins
	origin := r.Header.Get("Origin")
	for _, allowedOrigin := range h.allowedOrigins {
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			break
		}
	}

	// Create a channel for this client
	client := make(chan []byte, 256)

	// Register client
	h.register <- client

	// Ensure unregister on exit
	defer func() {
		h.unregister <- client
	}()

	// Send initial connection message
	fmt.Fprintf(w, "data: %s\n\n", `{"type":"sys","message":"connected"}`)
	w.(http.Flusher).Flush()

	// Listen for connection close
	notify := r.Context().Done()

	for {
		select {
		case <-notify:
			return
		case msg, ok := <-client:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			w.(http.Flusher).Flush()
		}
	}
}
