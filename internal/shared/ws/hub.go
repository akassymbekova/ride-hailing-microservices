package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"ride-hail/internal/shared/logging"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = 30 * time.Second
	authTimeout    = 5 * time.Second
	sendBufferSize = 16
)

type InboundMessage struct {
	ClientID string
	Data     []byte
}

type MessageHandler func(msg InboundMessage)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*client
	log     *logging.Logger
	onMsg   MessageHandler
}

type client struct {
	id   string
	conn *websocket.Conn
	send chan []byte
}

func NewHub(log *logging.Logger, onMsg MessageHandler) *Hub {
	return &Hub{
		clients: make(map[string]*client),
		log:     log,
		onMsg:   onMsg,
	}
}

type authMessage struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type TokenValidator func(clientID, token string) bool

func (h *Hub) HandleConnection(conn *websocket.Conn, clientID string, validate TokenValidator) {
	ctx := context.Background()

	_ = conn.SetReadDeadline(time.Now().Add(authTimeout))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		h.log.Error(ctx, "ws_auth_read_failed", "failed to read auth message", err)
		_ = conn.Close()
		return
	}

	var auth authMessage
	if err := json.Unmarshal(raw, &auth); err != nil || auth.Type != "auth" {
		h.log.Info(ctx, "ws_auth_invalid", "first message was not a valid auth message")
		_ = conn.Close()
		return
	}

	if validate != nil && !validate(clientID, auth.Token) {
		h.log.Info(ctx, "ws_auth_rejected", "token validation failed")
		_ = conn.Close()
		return
	}

	_ = conn.SetReadDeadline(time.Time{})

	c := &client{id: clientID, conn: conn, send: make(chan []byte, sendBufferSize)}

	h.mu.Lock()
	h.clients[clientID] = c
	h.mu.Unlock()

	h.log.Info(ctx, "ws_client_connected", "driver websocket authenticated")

	go h.writePump(c)
	h.readPump(c)
}

func (h *Hub) readPump(c *client) {
	defer h.disconnect(c)

	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if h.onMsg != nil {
			h.onMsg(InboundMessage{ClientID: c.id, Data: data})
		}
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case data, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) disconnect(c *client) {
	h.mu.Lock()
	delete(h.clients, c.id)
	h.mu.Unlock()
	close(c.send)
	_ = c.conn.Close()
	h.log.Info(context.Background(), "ws_client_disconnected", "driver websocket disconnected")
}

func (h *Hub) SendJSON(clientID string, v interface{}) bool {
	h.mu.RLock()
	c, ok := h.clients[clientID]
	h.mu.RUnlock()
	if !ok {
		return false
	}

	data, err := json.Marshal(v)
	if err != nil {
		return false
	}

	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

func (h *Hub) IsConnected(clientID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[clientID]
	return ok
}
