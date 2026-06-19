package server

import (
	"context"
	"database/sql"
	"github.com/alex4096/server-dashboard/backend/internal/audit"
	panelLogs "github.com/alex4096/server-dashboard/backend/internal/logs"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerFakeServer(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-server")
	source := "#!/bin/sh\necho ready\nwhile IFS= read -r line; do\n case \"$line\" in\n  list) echo 'players: 0';;\n  stop) echo stopping; exit 0;;\n esac\ndone\n"
	if err := os.WriteFile(script, []byte(source), 0750); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	schema := `CREATE TABLE server_runs(id INTEGER PRIMARY KEY AUTOINCREMENT,server_id TEXT,pid INTEGER,state TEXT,started_at DATETIME,stopped_at DATETIME,exit_code INTEGER,error_message TEXT,created_at DATETIME);CREATE TABLE log_entries(id INTEGER PRIMARY KEY AUTOINCREMENT,server_id TEXT,run_id INTEGER,level TEXT,source TEXT,message TEXT,created_at DATETIME);CREATE TABLE command_entries(id INTEGER PRIMARY KEY AUTOINCREMENT,server_id TEXT,run_id INTEGER,command TEXT,source TEXT,user_id TEXT,success BOOLEAN,error_message TEXT,created_at DATETIME);CREATE TABLE audit_events(id INTEGER PRIMARY KEY AUTOINCREMENT,actor_type TEXT,actor_id TEXT,action TEXT,target TEXT,detail TEXT,created_at DATETIME);`
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	lm := panelLogs.New(db, "test")
	defer lm.Close()
	entries, unsubscribe := lm.Subscribe()
	defer unsubscribe()
	m := New(db, audit.New(db), lm, "test", script, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitLog(t, ctx, entries, "ready")
	if err = m.SendCommand(ctx, "list", "test", "tester"); err != nil {
		t.Fatal(err)
	}
	waitLog(t, ctx, entries, "players: 0")
	if err = m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := m.Status(ctx)
	if st.State != StateStopped {
		t.Fatalf("state=%s", st.State)
	}
}
func waitLog(t *testing.T, ctx context.Context, entries <-chan panelLogs.Entry, want string) {
	t.Helper()
	for {
		select {
		case e := <-entries:
			if strings.Contains(e.Message, want) {
				return
			}
		case <-ctx.Done():
			t.Fatalf("log %q not received", want)
		}
	}
}
