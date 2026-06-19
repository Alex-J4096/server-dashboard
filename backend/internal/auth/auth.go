package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type Permission string

const (
	ReadServer    Permission = "server.read"
	ControlServer Permission = "server.control"
	SendCommand   Permission = "server.command"
	ReadLogs      Permission = "logs.read"
	ReadConfig    Permission = "config.read"
	WriteConfig   Permission = "config.write"
	ReadMetrics   Permission = "metrics.read"
	ReadAudit     Permission = "audit.read"
	ManageUsers   Permission = "users.manage"
)

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      Role      `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	db        *sql.DB
	ttl       time.Duration
	enabled   bool
	dummyHash []byte
}

var ErrInvalidCredentials = errors.New("用户名或密码错误")
var ErrUnauthorized = errors.New("未登录或会话已过期")
var ErrForbidden = errors.New("权限不足")

func New(db *sql.DB, ttl time.Duration, enabled bool) *Service {
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	return &Service{db: db, ttl: ttl, enabled: enabled, dummyHash: dummyHash}
}
func (s *Service) Enabled() bool { return s.enabled }

func ValidRole(role Role) bool {
	return role == RoleAdmin || role == RoleOperator || role == RoleViewer
}

func (u User) Can(p Permission) bool {
	if u.Role == RoleAdmin {
		return true
	}
	switch p {
	case ReadServer, ReadLogs, ReadConfig, ReadMetrics:
		return u.Role == RoleOperator || u.Role == RoleViewer
	case ControlServer, SendCommand, WriteConfig:
		return u.Role == RoleOperator
	default:
		return false
	}
}

func (s *Service) Bootstrap(ctx context.Context, username, password string) error {
	if !s.enabled {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if strings.TrimSpace(username) == "" || len(password) < 12 {
		return errors.New("首次启动必须设置 ADMIN_PASSWORD（至少 12 个字符）")
	}
	_, err := s.CreateUser(ctx, strings.TrimSpace(username), password, RoleAdmin)
	return err
}

func (s *Service) CreateUser(ctx context.Context, username, password string, role Role) (User, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return User{}, errors.New("用户名长度必须为 3-64 个字符")
	}
	if len(password) < 12 || len(password) > 128 {
		return User{}, errors.New("密码长度必须为 12-128 个字符")
	}
	if !ValidRole(role) {
		return User{}, errors.New("无效角色")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	u := User{ID: randomHex(16), Username: username, Role: role, CreatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,?,0,?,?)`, u.ID, u.Username, string(hash), u.Role, now, now)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return User{}, errors.New("用户名已存在")
	}
	return u, err
}

func (s *Service) Login(ctx context.Context, username, password string) (User, string, time.Time, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,role,disabled,created_at FROM users WHERE username=?`, strings.TrimSpace(username)).Scan(&u.ID, &u.Username, &hash, &u.Role, &u.Disabled, &u.CreatedAt)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}
	if u.Disabled || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, "", time.Time{}, ErrInvalidCredentials
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<=?`, time.Now().UTC())
	token := randomHex(32)
	expires := time.Now().UTC().Add(s.ttl)
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`, tokenHash(token), u.ID, expires, time.Now().UTC())
	return u, token, expires, err
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if !s.enabled {
		return User{ID: "local", Username: "local", Role: RoleAdmin}, nil
	}
	if token == "" {
		return User{}, ErrUnauthorized
	}
	var u User
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.role,u.disabled,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash(token), time.Now().UTC()).Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt)
	if err != nil || u.Disabled {
		return User{}, ErrUnauthorized
	}
	return u, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash(token))
	return err
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,role,disabled,created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) UpdateUser(ctx context.Context, actorID, id string, role Role, disabled bool) error {
	if !ValidRole(role) {
		return errors.New("无效角色")
	}
	if actorID == id && (role != RoleAdmin || disabled) {
		return errors.New("不能移除自己的管理员权限或禁用自己")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldRole Role
	var oldDisabled bool
	if err = tx.QueryRowContext(ctx, `SELECT role,disabled FROM users WHERE id=?`, id).Scan(&oldRole, &oldDisabled); err != nil {
		return err
	}
	if oldRole == RoleAdmin && !oldDisabled && (role != RoleAdmin || disabled) {
		var admins int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0`).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return errors.New("必须保留至少一个可用管理员")
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE users SET role=?,disabled=?,updated_at=? WHERE id=?`, role, disabled, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if disabled {
		if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) SetPassword(ctx context.Context, id, password string) error {
	if len(password) < 12 || len(password) > 128 {
		return errors.New("密码长度必须为 12-128 个字符")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`, string(h), time.Now().UTC(), id)
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id)
	}
	return err
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b)
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
