package server

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"go.mongodb.org/mongo-driver/v2/bson"
	"koffe/api/internal/models"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Type        string `json:"type"`
	Data        any    `json:"data,omitempty"`
	PublicToken string `json:"-"`
	Status      string `json:"-"`
}
type subscriber struct {
	role, publicToken string
	events            chan Event
	cancel            context.CancelFunc
}
type Hub struct {
	mu      sync.Mutex
	clients map[*subscriber]bool
}

func NewHub() *Hub { return &Hub{clients: map[*subscriber]bool{}} }
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.clients {
		allowed := s.role == "admin" || s.role == "cashier"
		if s.publicToken != "" {
			allowed = e.PublicToken == s.publicToken || e.Type == "menu.updated"
		}

		if !allowed {
			continue
		}
		select {
		case s.events <- e:
		default:
			s.cancel()
			delete(h.clients, s)
		}
	}
}
func (s *Server) orderEvent(kind string, o models.Order) {
	s.Hub.Publish(Event{Type: kind, Data: o, PublicToken: o.PublicToken, Status: o.Status})
	s.Hub.Publish(Event{Type: "table.updated", Data: map[string]any{"id": o.TableID}})
	if kind == "order.created" || kind == "order.ready" {
		s.Hub.Publish(Event{Type: "notification.created", Data: map[string]any{"orderNumber": o.OrderNumber, "tableNumber": o.TableNumber, "status": o.Status}, Status: o.Status})
	}
}
func (s *Server) socket(w http.ResponseWriter, r *http.Request) {
	// Require the configured browser origin, including the port. Non-browser clients may omit Origin.
	if origin := r.Header.Get("Origin"); origin != "" && !s.isAllowedOrigin(origin) {
		http.Error(w, "Forbidden", 403)
		return
	}
	conn, e := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if e != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8192)
	authCtx, authCancel := context.WithTimeout(r.Context(), 5*time.Second)
	var first struct{ Type, AccessToken, PublicToken string }
	e = wsjson.Read(authCtx, conn, &first)
	authCancel()
	if e != nil || first.Type != "authenticate" {
		_ = conn.Close(1008, "Authentication required")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sub := &subscriber{events: make(chan Event, 32), cancel: cancel}
	var expires time.Time
	if first.AccessToken != "" && first.PublicToken == "" {
		user, e := s.authenticate(ctx, first.AccessToken)
		if e != nil {
			_ = conn.Close(1008, "Session ended")
			return
		}
		sub.role = user.Role
		claims, _ := securityClaims(first.AccessToken, s.Config.JWTSecret)
		expires = claims
	} else if first.PublicToken != "" && first.AccessToken == "" {
		n, e := s.DB.Collection("orders").CountDocuments(ctx, bson.M{"publicToken": first.PublicToken})
		if e != nil || n != 1 {
			_ = conn.Close(1008, "Unknown order")
			return
		}
		sub.publicToken = first.PublicToken
	} else {
		_ = conn.Close(1008, "Invalid authentication")
		return
	}
	s.Hub.mu.Lock()
	if len(s.Hub.clients) >= 2000 {
		s.Hub.mu.Unlock()
		_ = conn.Close(1013, "Capacity reached")
		return
	}
	s.Hub.clients[sub] = true
	s.Hub.mu.Unlock()
	defer func() { s.Hub.mu.Lock(); delete(s.Hub.clients, sub); s.Hub.mu.Unlock() }()
	write := func(v any) error {
		writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		return wsjson.Write(writeCtx, conn, v)
	}
	if write(Event{Type: "connected"}) != nil {
		return
	}
	go func() {
		defer cancel()
		for {
			var message json.RawMessage
			if e := wsjson.Read(ctx, conn, &message); e != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-sub.events:
			if !expires.IsZero() && time.Now().After(expires) {
				_ = conn.Close(4001, "Session expired")
				return
			}
			if write(ev) != nil {
				return
			}
		case <-ticker.C:
			if sub.role != "" {
				check, done := context.WithTimeout(ctx, 5*time.Second)
				_, e := s.authenticate(check, first.AccessToken)
				done()
				if e != nil {
					_ = conn.Close(4001, "Session ended")
					return
				}
			}
			ping, done := context.WithTimeout(ctx, 5*time.Second)
			e := conn.Ping(ping)
			done()
			if e != nil {
				return
			}
		}
	}
}
func sortPopular(p []bson.M) {
	sort.Slice(p, func(i, j int) bool {
		a, b := p[i]["quantity"].(int64), p[j]["quantity"].(int64)
		if a == b {
			return strings.Compare(p[i]["name"].(string), p[j]["name"].(string)) < 0
		}
		return a > b
	})
}
func slogJobError() { slog.Error("order expiration job failed; retrying next tick") }
