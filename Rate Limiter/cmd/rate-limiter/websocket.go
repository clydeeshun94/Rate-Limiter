package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"rate-limiter/internal/metrics"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type wsHub struct {
	mu        sync.RWMutex
	clients   map[*wsClient]bool
	collector *metrics.Collector
	ticker    *time.Ticker
	stop      chan bool
}

func newWSHub(collector *metrics.Collector) *wsHub {
	h := &wsHub{
		clients:   make(map[*wsClient]bool),
		collector: collector,
		ticker:    time.NewTicker(1 * time.Second),
		stop:      make(chan bool),
	}
	go h.run()
	return h
}

func (h *wsHub) run() {
	for {
		select {
		case <-h.stop:
			h.ticker.Stop()
			return
		case <-h.ticker.C:
			h.broadcast()
		}
	}
}

func (h *wsHub) broadcast() {
	snap := h.collector.Snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}

	h.mu.RLock()
	dead := make([]*wsClient, 0)
	for client := range h.clients {
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()
		if err != nil {
			dead = append(dead, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range dead {
		_ = client.conn.Close()
		h.removeClient(client)
	}
}

func (h *wsHub) addClient(conn *websocket.Conn) *wsClient {
	client := &wsClient{conn: conn}
	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()
	go h.handleClient(client)
	return client
}

func (h *wsHub) removeClient(client *wsClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	_ = client.conn.Close()
}

func (h *wsHub) handleClient(client *wsClient) {
	defer h.removeClient(client)
	for {
		_, _, err := client.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (h *wsHub) stopHub() {
	close(h.stop)
}

func (s *service) handleWSMetrics(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.wsHub.addClient(conn)
}