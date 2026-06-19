package logs

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Entry struct {
	ID        int64     `json:"id,omitempty"`
	ServerID  string    `json:"server_id"`
	RunID     *int64    `json:"run_id,omitempty"`
	Level     string    `json:"level"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type Manager struct {
	db       *sql.DB
	serverID string
	in       chan Entry
	done     chan struct{}
	stopped  chan struct{}
	once     sync.Once
	mu       sync.RWMutex
	clients  map[chan Entry]struct{}
	total    atomic.Uint64
}

func New(db *sql.DB, serverID string) *Manager {
	m := &Manager{db: db, serverID: serverID, in: make(chan Entry, 2048), done: make(chan struct{}), stopped: make(chan struct{}), clients: map[chan Entry]struct{}{}}
	go m.writer()
	return m
}

func ParseLevel(source, message string) string {
	s := strings.ToLower(message)
	if source == "stderr" || strings.Contains(s, "error") || strings.Contains(s, "fatal") {
		return "error"
	}
	if strings.Contains(s, "warn") {
		return "warn"
	}
	return "info"
}

func (m *Manager) Publish(runID *int64, source, message string) {
	e := Entry{ServerID: m.serverID, RunID: runID, Level: ParseLevel(source, message), Source: source, Message: message, CreatedAt: time.Now().UTC()}
	m.total.Add(1)
	m.mu.RLock()
	for c := range m.clients {
		select {
		case c <- e:
		default:
		}
	}
	m.mu.RUnlock()
	select {
	case m.in <- e:
	default:
	}
}

func (m *Manager) Subscribe() (<-chan Entry, func()) {
	c := make(chan Entry, 256)
	m.mu.Lock()
	m.clients[c] = struct{}{}
	m.mu.Unlock()
	return c, func() {
		m.mu.Lock()
		if _, ok := m.clients[c]; ok {
			delete(m.clients, c)
			close(c)
		}
		m.mu.Unlock()
	}
}
func (m *Manager) ClientCount() int { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.clients) }
func (m *Manager) Total() uint64    { return m.total.Load() }
func (m *Manager) Close()           { m.once.Do(func() { close(m.done); <-m.stopped }) }
func (m *Manager) writer() {
	defer close(m.stopped)
	for {
		select {
		case e := <-m.in:
			_, _ = m.db.Exec(`INSERT INTO log_entries(server_id,run_id,level,source,message,created_at) VALUES(?,?,?,?,?,?)`, e.ServerID, e.RunID, e.Level, e.Source, e.Message, e.CreatedAt)
		case <-m.done:
			return
		}
	}
}

func (m *Manager) List(ctx context.Context, q string, limit int) ([]Entry, error) {
	if limit < 1 || limit > 2000 {
		limit = 500
	}
	query := `SELECT id,server_id,run_id,level,source,message,created_at FROM log_entries WHERE server_id=?`
	args := []any{m.serverID}
	if q != "" {
		query += ` AND message LIKE ?`
		args = append(args, "%"+q+"%")
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.ServerID, &e.RunID, &e.Level, &e.Source, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (m *Manager) Cleanup(ctx context.Context, days, maxRows int) error {
	if days > 0 {
		_, _ = m.db.ExecContext(ctx, `DELETE FROM log_entries WHERE server_id=? AND created_at < ?`, m.serverID, time.Now().UTC().Add(-time.Duration(days)*24*time.Hour))
	}
	if maxRows > 0 {
		_, _ = m.db.ExecContext(ctx, `DELETE FROM log_entries WHERE server_id=? AND id NOT IN (SELECT id FROM log_entries WHERE server_id=? ORDER BY id DESC LIMIT ?)`, m.serverID, m.serverID, maxRows)
	}
	return nil
}
