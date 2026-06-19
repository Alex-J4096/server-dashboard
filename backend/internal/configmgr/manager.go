package configmgr

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alex4096/server-dashboard/backend/internal/audit"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrNotAllowed = errors.New("config file is not allowed")
var ErrInvalidPath = errors.New("config path is invalid")
var allowed = map[string]struct{}{"server.properties": {}, "allowlist.json": {}, "permissions.json": {}}

type Snapshot struct {
	ID          int64     `json:"id"`
	ServerID    string    `json:"server_id"`
	FilePath    string    `json:"file_path"`
	Content     string    `json:"content,omitempty"`
	ContentHash string    `json:"content_hash"`
	CreatedBy   *string   `json:"created_by"`
	Reason      *string   `json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
}
type Manager struct {
	root, serverID string
	db             *sql.DB
	audit          *audit.Repository
}

func New(root, serverID string, db *sql.DB, a *audit.Repository) *Manager {
	return &Manager{root: filepath.Clean(root), serverID: serverID, db: db, audit: a}
}
func (m *Manager) Files() []string {
	return []string{"server.properties", "allowlist.json", "permissions.json"}
}
func (m *Manager) SafePath(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || filepath.Clean(name) != name || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		return "", ErrInvalidPath
	}
	if _, ok := allowed[name]; !ok {
		return "", ErrNotAllowed
	}
	p := filepath.Join(m.root, name)
	rel, err := filepath.Rel(m.root, p)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", ErrInvalidPath
	}
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidPath
	}
	return p, nil
}
func (m *Manager) Read(ctx context.Context, name string) (string, error) {
	p, err := m.SafePath(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err == nil {
		_ = m.audit.Record(ctx, "user", "local", "config.read", name, "")
	}
	return string(b), err
}
func Validate(name, content string) error {
	switch name {
	case "server.properties":
		return ValidateServerProperties(content)
	case "allowlist.json", "permissions.json":
		return ValidateJSON(content)
	default:
		return ErrNotAllowed
	}
}
func ValidateServerProperties(content string) error {
	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("line %d must contain key=value", i+1)
		}
		switch strings.TrimSpace(k) {
		case "server-port", "server-portv6":
			n, e := strconv.Atoi(strings.TrimSpace(v))
			if e != nil || n < 1 || n > 65535 {
				return fmt.Errorf("line %d has invalid port", i+1)
			}
		case "max-players":
			n, e := strconv.Atoi(strings.TrimSpace(v))
			if e != nil || n < 1 {
				return fmt.Errorf("line %d has invalid max-players", i+1)
			}
		}
	}
	return nil
}
func ValidateJSON(content string) error {
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
func (m *Manager) Save(ctx context.Context, name, content, reason string) error {
	p, err := m.SafePath(name)
	if err != nil {
		return err
	}
	if err = Validate(name, content); err != nil {
		return err
	}
	old, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(old)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO config_snapshots(server_id,file_path,content,content_hash,created_by,reason,created_at) VALUES(?,?,?,?,?,?,?)`, m.serverID, name, string(old), hex.EncodeToString(sum[:]), "local", reason, time.Now().UTC()); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.root, ".mc-panel-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0640); err == nil {
		_, err = tmp.WriteString(content)
	}
	if e := tmp.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, p); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return m.audit.Record(ctx, "user", "local", "config.update", name, reason)
}
func (m *Manager) History(ctx context.Context, name string, limit int) ([]Snapshot, error) {
	if _, err := m.SafePath(name); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := m.db.QueryContext(ctx, `SELECT id,server_id,file_path,content_hash,created_by,reason,created_at FROM config_snapshots WHERE server_id=? AND file_path=? ORDER BY id DESC LIMIT ?`, m.serverID, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		var s Snapshot
		if err := rows.Scan(&s.ID, &s.ServerID, &s.FilePath, &s.ContentHash, &s.CreatedBy, &s.Reason, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (m *Manager) Restore(ctx context.Context, id int64) error {
	var s Snapshot
	if err := m.db.QueryRowContext(ctx, `SELECT id,server_id,file_path,content,content_hash,created_by,reason,created_at FROM config_snapshots WHERE id=? AND server_id=?`, id, m.serverID).Scan(&s.ID, &s.ServerID, &s.FilePath, &s.Content, &s.ContentHash, &s.CreatedBy, &s.Reason, &s.CreatedAt); err != nil {
		return err
	}
	if err := m.Save(ctx, s.FilePath, s.Content, fmt.Sprintf("restore snapshot %d", id)); err != nil {
		return err
	}
	return m.audit.Record(ctx, "user", "local", "config.restore", s.FilePath, fmt.Sprintf("snapshot_id=%d", id))
}
