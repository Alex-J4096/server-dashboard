package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE users(id TEXT PRIMARY KEY,username TEXT UNIQUE,password_hash TEXT,role TEXT,disabled BOOLEAN,created_at DATETIME,updated_at DATETIME);CREATE TABLE sessions(token_hash TEXT PRIMARY KEY,user_id TEXT,expires_at DATETIME,created_at DATETIME);`)
	if err != nil {
		t.Fatal(err)
	}
	return New(db, time.Hour, true)
}

func TestLoginAndPermissions(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if err := s.Bootstrap(ctx, "admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	u, token, _, err := s.Login(ctx, "admin", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != RoleAdmin || token == "" {
		t.Fatal("unexpected login result")
	}
	if _, err = s.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	viewer, err := s.CreateUser(ctx, "viewer", "viewer-password-123", RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if !viewer.Can(ReadLogs) || viewer.Can(ControlServer) {
		t.Fatal("viewer permissions are invalid")
	}
}
func TestSelfAdminCannotDisable(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "admin", "correct-horse-battery", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateUser(ctx, u.ID, u.ID, RoleViewer, true); err == nil {
		t.Fatal("expected self protection error")
	}
}
