package ride

import (
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"ride-hail/internal/shared/auth"
	"ride-hail/internal/shared/logging"
	"ride-hail/internal/shared/ws"
)

type WebSocketHandler struct {
	hub *ws.Hub
	log *logging.Logger
}

func NewWebSocketHandler(hub *ws.Hub, log *logging.Logger) *WebSocketHandler {
	return &WebSocketHandler{hub: hub, log: log}
}

var passengerUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func (h *WebSocketHandler) HandlePassengerWS(w http.ResponseWriter, r *http.Request) {
	passengerID := extractPassengerID(r.URL.Path)
	if passengerID == "" {
		http.Error(w, "passenger_id is required", http.StatusBadRequest)
		return
	}

	conn, err := passengerUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error(r.Context(), "ws_upgrade_failed", "failed to upgrade passenger websocket", err)
		return
	}

	validate := func(clientID, token string) bool {
		claims, err := auth.ParseBearerToken(token)
		if err != nil {
			return false
		}
		if strings.ToUpper(claims.Role) != "PASSENGER" {
			return false
		}
		return claims.Subject == clientID
	}

	h.hub.HandleConnection(conn, passengerID, validate)
}

func extractPassengerID(path string) string {
	const prefix = "/ws/passengers/"
	rest := strings.TrimPrefix(path, prefix)
	if idx := strings.Index(rest, "/"); idx != -1 {
		return rest[:idx]
	}
	return strings.TrimSpace(rest)
}
