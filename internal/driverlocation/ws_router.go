package driverlocation

import (
	"encoding/json"

	"ride-hail/internal/shared/ws"
)

type WSMessageRouter struct {
	matcher *Matcher
	handler *Handler
}

func NewWSMessageRouter(matcher *Matcher, handler *Handler) *WSMessageRouter {
	return &WSMessageRouter{matcher: matcher, handler: handler}
}

func (r *WSMessageRouter) Handle(msg ws.InboundMessage) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(msg.Data, &envelope); err != nil {
		return
	}

	switch envelope.Type {
	case "ride_response":
		r.matcher.HandleDriverResponse(msg)
	case "status_update":
		r.handler.HandleWSStatusUpdate(msg)
	case "location_update":
		r.handler.HandleWSLocationUpdate(msg)
	}
}
