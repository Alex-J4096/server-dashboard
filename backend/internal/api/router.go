package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alex4096/server-dashboard/backend/internal/audit"
	"github.com/alex4096/server-dashboard/backend/internal/auth"
	"github.com/alex4096/server-dashboard/backend/internal/configmgr"
	panelLogs "github.com/alex4096/server-dashboard/backend/internal/logs"
	panelMetrics "github.com/alex4096/server-dashboard/backend/internal/metrics"
	"github.com/alex4096/server-dashboard/backend/internal/server"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Router struct {
	server        *server.Manager
	logs          *panelLogs.Manager
	config        *configmgr.Manager
	metrics       *panelMetrics.Collector
	audit         *audit.Repository
	auth          *auth.Service
	serverID      string
	upgrader      websocket.Upgrader
	loginMu       sync.Mutex
	loginAttempts map[string][]time.Time
}

func New(s *server.Manager, l *panelLogs.Manager, c *configmgr.Manager, m *panelMetrics.Collector, a *audit.Repository, authService *auth.Service, serverID string) *Router {
	return &Router{server: s, logs: l, config: c, metrics: m, audit: a, auth: authService, serverID: serverID, loginAttempts: make(map[string][]time.Time), upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return sameOrigin(r) }}}
}

func (r *Router) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) { success(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/auth/login", r.login)
	mux.Handle("GET /api/auth/me", r.require(auth.ReadServer, http.HandlerFunc(r.me)))
	mux.Handle("POST /api/auth/logout", r.require(auth.ReadServer, http.HandlerFunc(r.logout)))
	mux.Handle("GET /api/server/status", r.require(auth.ReadServer, http.HandlerFunc(r.status)))
	mux.Handle("POST /api/server/start", r.require(auth.ControlServer, r.action("server.start", r.server.Start)))
	mux.Handle("POST /api/server/stop", r.require(auth.ControlServer, r.action("server.stop", r.server.Stop)))
	mux.Handle("POST /api/server/restart", r.require(auth.ControlServer, r.action("server.restart", r.server.Restart)))
	mux.Handle("POST /api/server/command", r.require(auth.SendCommand, http.HandlerFunc(r.command)))
	mux.Handle("GET /api/logs", r.require(auth.ReadLogs, http.HandlerFunc(r.listLogs)))
	mux.Handle("GET /api/logs/search", r.require(auth.ReadLogs, http.HandlerFunc(r.listLogs)))
	mux.Handle("GET /api/logs/stream", r.require(auth.ReadLogs, http.HandlerFunc(r.stream)))
	mux.Handle("GET /api/config/files", r.require(auth.ReadConfig, http.HandlerFunc(r.configFiles)))
	mux.Handle("GET /api/config/file", r.require(auth.ReadConfig, http.HandlerFunc(r.configRead)))
	mux.Handle("PUT /api/config/file", r.require(auth.WriteConfig, http.HandlerFunc(r.configSave)))
	mux.Handle("POST /api/config/validate", r.require(auth.WriteConfig, http.HandlerFunc(r.configValidate)))
	mux.Handle("GET /api/config/history", r.require(auth.ReadConfig, http.HandlerFunc(r.configHistory)))
	mux.Handle("POST /api/config/restore", r.require(auth.WriteConfig, http.HandlerFunc(r.configRestore)))
	mux.Handle("GET /api/metrics/summary", r.require(auth.ReadMetrics, http.HandlerFunc(r.summary)))
	mux.Handle("GET /api/audit", r.require(auth.ReadAudit, http.HandlerFunc(r.auditList)))
	mux.Handle("GET /api/users", r.require(auth.ManageUsers, http.HandlerFunc(r.usersList)))
	mux.Handle("POST /api/users", r.require(auth.ManageUsers, http.HandlerFunc(r.usersCreate)))
	mux.Handle("PUT /api/users/{id}", r.require(auth.ManageUsers, http.HandlerFunc(r.usersUpdate)))
	mux.Handle("PUT /api/users/{id}/password", r.require(auth.ManageUsers, http.HandlerFunc(r.usersPassword)))
	mux.Handle("GET /metrics", r.prometheus())
	return recoverer(cors(mux))
}

type userContextKey struct{}

func currentUser(req *http.Request) auth.User {
	u, _ := req.Context().Value(userContextKey{}).(auth.User)
	return u
}
func sessionToken(req *http.Request) string {
	c, err := req.Cookie("panel_session")
	if err == nil {
		return c.Value
	}
	if v := req.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		return strings.TrimPrefix(v, "Bearer ")
	}
	return ""
}

func (r *Router) require(permission auth.Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		u, err := r.auth.Authenticate(req.Context(), sessionToken(req))
		if err != nil {
			failure(w, "UNAUTHORIZED", auth.ErrUnauthorized, http.StatusUnauthorized)
			return
		}
		if !u.Can(permission) {
			_ = r.audit.Record(req.Context(), "user", u.ID, "auth.denied", string(permission), req.URL.Path)
			failure(w, "FORBIDDEN", auth.ErrForbidden, http.StatusForbidden)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead && !sameOrigin(req) {
			failure(w, "CSRF_REJECTED", errors.New("请求来源无效"), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), userContextKey{}, u)))
	})
}

func (r *Router) login(w http.ResponseWriter, req *http.Request) {
	if !sameOrigin(req) {
		failure(w, "CSRF_REJECTED", errors.New("请求来源无效"), http.StatusForbidden)
		return
	}
	if r.tooManyLoginAttempts(clientIP(req)) {
		failure(w, "RATE_LIMITED", errors.New("登录尝试过多，请稍后再试"), http.StatusTooManyRequests)
		return
	}
	var v struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	u, token, expires, err := r.auth.Login(req.Context(), v.Username, v.Password)
	if err != nil {
		r.recordLoginFailure(clientIP(req))
		_ = r.audit.Record(req.Context(), "anonymous", clientIP(req), "auth.login_failed", v.Username, "")
		failure(w, "INVALID_CREDENTIALS", auth.ErrInvalidCredentials, http.StatusUnauthorized)
		return
	}
	r.clearLoginAttempts(clientIP(req))
	secure := req.TLS != nil || req.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{Name: "panel_session", Value: token, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	_ = r.audit.Record(req.Context(), "user", u.ID, "auth.login", "panel", clientIP(req))
	success(w, u)
}
func (r *Router) me(w http.ResponseWriter, req *http.Request) { success(w, currentUser(req)) }
func (r *Router) logout(w http.ResponseWriter, req *http.Request) {
	u := currentUser(req)
	err := r.auth.Logout(req.Context(), sessionToken(req))
	http.SetCookie(w, &http.Cookie{Name: "panel_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	_ = r.audit.Record(req.Context(), "user", u.ID, "auth.logout", "panel", "")
	respond(w, map[string]bool{"logged_out": true}, err)
}

func (r *Router) usersList(w http.ResponseWriter, req *http.Request) {
	v, err := r.auth.ListUsers(req.Context())
	respond(w, v, err)
}
func (r *Router) usersCreate(w http.ResponseWriter, req *http.Request) {
	var v struct {
		Username string    `json:"username"`
		Password string    `json:"password"`
		Role     auth.Role `json:"role"`
	}
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	u, err := r.auth.CreateUser(req.Context(), v.Username, v.Password, v.Role)
	if err == nil {
		actor := currentUser(req)
		_ = r.audit.Record(req.Context(), "user", actor.ID, "user.create", u.ID, string(u.Role))
	}
	respond(w, u, err)
}
func (r *Router) usersUpdate(w http.ResponseWriter, req *http.Request) {
	var v struct {
		Role     auth.Role `json:"role"`
		Disabled bool      `json:"disabled"`
	}
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	actor := currentUser(req)
	id := req.PathValue("id")
	err := r.auth.UpdateUser(req.Context(), actor.ID, id, v.Role, v.Disabled)
	if err == nil {
		_ = r.audit.Record(req.Context(), "user", actor.ID, "user.update", id, fmt.Sprintf("role=%s disabled=%v", v.Role, v.Disabled))
	}
	respond(w, map[string]bool{"updated": true}, err)
}
func (r *Router) usersPassword(w http.ResponseWriter, req *http.Request) {
	var v struct {
		Password string `json:"password"`
	}
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	actor := currentUser(req)
	id := req.PathValue("id")
	err := r.auth.SetPassword(req.Context(), id, v.Password)
	if err == nil {
		_ = r.audit.Record(req.Context(), "user", actor.ID, "user.password_reset", id, "")
	}
	respond(w, map[string]bool{"updated": true}, err)
}

func clientIP(req *http.Request) string {
	if v := req.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return host
	}
	return req.RemoteAddr
}
func (r *Router) tooManyLoginAttempts(ip string) bool {
	r.loginMu.Lock()
	defer r.loginMu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	valid := r.loginAttempts[ip][:0]
	for _, t := range r.loginAttempts[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	r.loginAttempts[ip] = valid
	return len(valid) >= 5
}
func (r *Router) recordLoginFailure(ip string) {
	r.loginMu.Lock()
	defer r.loginMu.Unlock()
	if len(r.loginAttempts) > 10000 {
		r.loginAttempts = make(map[string][]time.Time)
	}
	r.loginAttempts[ip] = append(r.loginAttempts[ip], time.Now())
}
func (r *Router) clearLoginAttempts(ip string) {
	r.loginMu.Lock()
	defer r.loginMu.Unlock()
	delete(r.loginAttempts, ip)
}
func (r *Router) status(w http.ResponseWriter, req *http.Request) {
	v, err := r.server.Status(req.Context())
	respond(w, v, err)
}
func (r *Router) action(action string, fn func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 25*time.Second)
		defer cancel()
		err := fn(ctx)
		if err == nil {
			u := currentUser(req)
			_ = r.audit.Record(req.Context(), "user", u.ID, action, r.serverID, "authenticated request")
		}
		respond(w, map[string]bool{"accepted": true}, err)
	}
}
func (r *Router) command(w http.ResponseWriter, req *http.Request) {
	var v struct {
		Command string `json:"command"`
	}
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, http.StatusBadRequest)
		return
	}
	respond(w, map[string]bool{"sent": true}, r.server.SendCommand(req.Context(), v.Command, "web", currentUser(req).ID))
}
func (r *Router) listLogs(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	v, err := r.logs.List(req.Context(), req.URL.Query().Get("q"), limit)
	respond(w, v, err)
}
func (r *Router) configFiles(w http.ResponseWriter, _ *http.Request) { success(w, r.config.Files()) }
func (r *Router) configRead(w http.ResponseWriter, req *http.Request) {
	name := req.URL.Query().Get("path")
	v, err := r.config.Read(req.Context(), name)
	respond(w, map[string]string{"path": name, "content": v}, err)
}

type configRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Reason  string `json:"reason"`
}

func (r *Router) configSave(w http.ResponseWriter, req *http.Request) {
	var v configRequest
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	err := r.config.Save(req.Context(), v.Path, v.Content, v.Reason)
	if err == nil {
		u := currentUser(req)
		_ = r.audit.Record(req.Context(), "user", u.ID, "config.update", v.Path, v.Reason)
	}
	respond(w, map[string]bool{"saved": true}, err)
}
func (r *Router) configValidate(w http.ResponseWriter, req *http.Request) {
	var v configRequest
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	respond(w, map[string]bool{"valid": true}, configmgr.Validate(v.Path, v.Content))
}
func (r *Router) configHistory(w http.ResponseWriter, req *http.Request) {
	v, err := r.config.History(req.Context(), req.URL.Query().Get("path"), 50)
	respond(w, v, err)
}
func (r *Router) configRestore(w http.ResponseWriter, req *http.Request) {
	var v struct {
		SnapshotID int64 `json:"snapshot_id"`
	}
	if err := decode(req, &v); err != nil {
		failure(w, "INVALID_REQUEST", err, 400)
		return
	}
	err := r.config.Restore(req.Context(), v.SnapshotID)
	if err == nil {
		u := currentUser(req)
		_ = r.audit.Record(req.Context(), "user", u.ID, "config.restore", fmt.Sprint(v.SnapshotID), "")
	}
	respond(w, map[string]bool{"restored": true}, err)
}
func (r *Router) summary(w http.ResponseWriter, req *http.Request) {
	v, err := r.metrics.Summary(req.Context())
	respond(w, v, err)
}
func (r *Router) auditList(w http.ResponseWriter, req *http.Request) {
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	v, err := r.audit.List(req.Context(), limit)
	respond(w, v, err)
}

func (r *Router) stream(w http.ResponseWriter, req *http.Request) {
	c, err := r.upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	defer c.Close()
	entries, unsubscribe := r.logs.Subscribe()
	defer unsubscribe()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var msg struct {
				Type string `json:"type"`
				Data struct {
					Command string `json:"command"`
				} `json:"data"`
			}
			if c.ReadJSON(&msg) != nil {
				return
			}
			if msg.Type == "command" {
				u := currentUser(req)
				if u.Can(auth.SendCommand) {
					_ = r.server.SendCommand(req.Context(), msg.Data.Command, "websocket", u.ID)
				}
			}
		}
	}()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-entries:
			if !ok {
				return
			}
			if c.WriteJSON(map[string]any{"type": "log", "data": e}) != nil {
				return
			}
		case <-ticker.C:
			if c.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		case <-readDone:
			return
		case <-req.Context().Done():
			return
		}
	}
}

func (r *Router) prometheus() http.Handler {
	reg := prometheus.NewRegistry()
	labels := prometheus.Labels{"server_id": r.serverID}
	gauge := func(name, help string, f func(panelMetrics.Summary) float64) {
		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: labels}, func() float64 { s, _ := r.metrics.Summary(context.Background()); return f(s) }))
	}
	gauge("mc_server_running", "Whether the Bedrock server is running", func(s panelMetrics.Summary) float64 {
		if s.Server.State == server.StateRunning {
			return 1
		}
		return 0
	})
	gauge("mc_server_uptime_seconds", "Server uptime", func(s panelMetrics.Summary) float64 { return float64(s.Server.UptimeSeconds) })
	gauge("mc_process_cpu_percent", "Bedrock process CPU percent", func(s panelMetrics.Summary) float64 { return s.Process.CPUPercent })
	gauge("mc_process_memory_bytes", "Bedrock process resident memory", func(s panelMetrics.Summary) float64 { return float64(s.Process.MemoryBytes) })
	gauge("mc_system_cpu_percent", "System CPU percent", func(s panelMetrics.Summary) float64 { return s.System.CPUPercent })
	gauge("mc_system_memory_used_bytes", "System memory used", func(s panelMetrics.Summary) float64 { return float64(s.System.MemoryUsed) })
	gauge("mc_system_memory_total_bytes", "System memory total", func(s panelMetrics.Summary) float64 { return float64(s.System.MemoryTotal) })
	gauge("mc_network_rx_bytes_total", "Network bytes received", func(s panelMetrics.Summary) float64 { return float64(s.Network.RXBytesTotal) })
	gauge("mc_network_tx_bytes_total", "Network bytes sent", func(s panelMetrics.Summary) float64 { return float64(s.Network.TXBytesTotal) })
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "mc_panel_ws_clients", Help: "Connected WebSocket clients", ConstLabels: labels}, func() float64 { return float64(r.logs.ClientCount()) }))
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "mc_server_log_lines_total", Help: "Log lines observed", ConstLabels: labels}, func() float64 { return float64(r.logs.Total()) }))
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

func decode(req *http.Request, v any) error {
	defer req.Body.Close()
	d := json.NewDecoder(io.LimitReader(req.Body, 2<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func success(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}
func failure(w http.ResponseWriter, code string, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": err.Error()}})
}
func respond(w http.ResponseWriter, data any, err error) {
	if err == nil {
		success(w, data)
		return
	}
	code, status := "INTERNAL_ERROR", 500
	switch {
	case errors.Is(err, server.ErrAlreadyRunning):
		code, status = "SERVER_ALREADY_RUNNING", 409
	case errors.Is(err, server.ErrNotRunning):
		code, status = "SERVER_NOT_RUNNING", 409
	case errors.Is(err, configmgr.ErrNotAllowed):
		code, status = "CONFIG_FILE_NOT_ALLOWED", 400
	case errors.Is(err, configmgr.ErrInvalidPath):
		code, status = "CONFIG_PATH_INVALID", 400
	case errors.Is(err, sql.ErrNoRows):
		code, status = "NOT_FOUND", 404
	case strings.Contains(err.Error(), "用户名") || strings.Contains(err.Error(), "密码") || strings.Contains(err.Error(), "角色") || strings.Contains(err.Error(), "管理员"):
		code, status = "VALIDATION_FAILED", 400
	case strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "line "):
		code, status = "CONFIG_VALIDATE_FAILED", 400
	}
	failure(w, code, err, status)
}
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				failure(w, "INTERNAL_ERROR", fmt.Errorf("internal server error"), 500)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
