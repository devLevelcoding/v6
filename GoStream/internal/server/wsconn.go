package server

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/levelcodingdev/gostream/internal/hub"
	"github.com/levelcodingdev/gostream/internal/proto"
	"github.com/levelcodingdev/gostream/internal/ws"
)

func (s *server) serveWS(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WSToken != "" && !wsAuthorized(r, s.cfg.WSToken) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	conn, err := ws.Upgrade(w, r)
	if err != nil {
		// Upgrade already wrote a response on a bad handshake.
		return
	}
	conn.SetReadLimit(s.cfg.ReadLimit)
	conn.SetIdleTimeout(s.cfg.IdleTimeout)

	client := s.cfg.Hub.Add(hub.Meta{
		Subject:    subjectFromToken(r),
		RemoteAddr: conn.RemoteAddr().String(),
	})
	s.cfg.Log.Info("ws connect", "client", client.ID, "remote", client.Meta.RemoteAddr)

	// Auto-subscribe from ?topics=a,b,c
	for _, t := range splitTopics(r.URL.Query().Get("topics")) {
		s.cfg.Hub.Subscribe(client, t)
	}
	_ = conn.WriteMessage(ws.OpText, proto.Welcome(client.ID))

	go s.guard("writeLoop", client, func() { s.writeLoop(conn, client) })
	s.guard("readLoop", client, func() { s.readLoop(conn, client) }) // blocks until the socket ends
	s.cfg.Hub.Remove(client)
	s.cfg.Log.Info("ws disconnect", "client", client.ID, "reason", client.KillReason())
}

// guard contains a panic in one connection's loop (CoverGo P3): the client is
// killed (so its sibling loop unblocks and cleanup runs) and the server keeps
// serving everyone else. An unrecovered panic in the write goroutine would
// otherwise take the whole process down.
func (s *server) guard(loop string, c *hub.Client, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			s.cfg.Log.Error("ws loop panic", "loop", loop, "client", c.ID,
				"recover", rec, "stack", string(debug.Stack()))
			c.Kill("panic in " + loop)
		}
	}()
	fn()
}

// writeLoop drains the client's outbound queue to the socket and pings on an
// interval. It returns when the client is killed or a write fails.
func (s *server) writeLoop(conn *ws.Conn, c *hub.Client) {
	ping := time.NewTicker(s.cfg.PingInterval)
	defer ping.Stop()
	for {
		select {
		case msg := <-c.Out():
			if err := conn.WriteMessage(ws.OpText, msg); err != nil {
				return
			}
		case <-ping.C:
			if err := conn.Ping(); err != nil {
				return
			}
		case <-c.Done():
			_ = conn.Close(ws.CloseGoingAway, c.KillReason())
			return
		}
	}
}

// readLoop handles inbound commands until the socket closes.
func (s *server) readLoop(conn *ws.Conn, c *hub.Client) {
	for {
		op, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if op != ws.OpText {
			continue // ignore binary in Phase 0
		}
		var cmd proto.Command
		if err := json.Unmarshal(data, &cmd); err != nil {
			_ = conn.WriteMessage(ws.OpText, proto.Errorf("bad command JSON"))
			continue
		}
		s.handleCommand(conn, c, cmd)
	}
}

func (s *server) handleCommand(conn *ws.Conn, c *hub.Client, cmd proto.Command) {
	switch cmd.Type {
	case "subscribe":
		if cmd.Topic == "" {
			_ = conn.WriteMessage(ws.OpText, proto.Errorf("subscribe needs a topic"))
			return
		}
		s.cfg.Hub.Subscribe(c, cmd.Topic)
		_ = conn.WriteMessage(ws.OpText, proto.Ack("subscribed", cmd.Topic))
	case "unsubscribe":
		s.cfg.Hub.Unsubscribe(c, cmd.Topic)
		_ = conn.WriteMessage(ws.OpText, proto.Ack("unsubscribed", cmd.Topic))
	case "publish":
		if !s.cfg.AllowClientPublish {
			_ = conn.WriteMessage(ws.OpText, proto.Errorf("client publish is disabled"))
			return
		}
		if cmd.Topic == "" {
			_ = conn.WriteMessage(ws.OpText, proto.Errorf("publish needs a topic"))
			return
		}
		s.cfg.Hub.Publish(cmd.Topic, proto.Message(cmd.Topic, cmd.Data))
	case "ping":
		_ = conn.WriteMessage(ws.OpText, proto.Pong())
	default:
		_ = conn.WriteMessage(ws.OpText, proto.Errorf("unknown command %q", cmd.Type))
	}
}

func splitTopics(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func wsAuthorized(r *http.Request, token string) bool {
	if t := r.URL.Query().Get("token"); t != "" {
		return t == token
	}
	auth := r.Header.Get("Authorization")
	return len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") && auth[7:] == token
}

// subjectFromToken is a Phase-0 placeholder: the identity is just the token
// value when present. Phase 2 swaps this for real JWT claims.
func subjectFromToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return "token:" + shortHash(t)
	}
	return ""
}

func shortHash(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[:3] + "…" + s[len(s)-3:]
}
