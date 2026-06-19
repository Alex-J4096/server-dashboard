package server

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alex4096/server-dashboard/backend/internal/audit"
	panelLogs "github.com/alex4096/server-dashboard/backend/internal/logs"
)

var (
	ErrAlreadyRunning = errors.New("server is already running")
	ErrNotRunning     = errors.New("server is not running")
)

type Manager struct {
	mu                               sync.Mutex
	db                               *sql.DB
	audit                            *audit.Repository
	logs                             *panelLogs.Manager
	serverID, executable, workingDir string
	state                            State
	cmd                              *exec.Cmd
	stdin                            io.WriteCloser
	runID                            int64
	startedAt                        time.Time
	done                             chan struct{}
	stopping                         bool
	restarts, crashes, commands      atomic.Uint64
}

func New(db *sql.DB, a *audit.Repository, l *panelLogs.Manager, id, executable, workingDir string) *Manager {
	return &Manager{db: db, audit: a, logs: l, serverID: id, executable: executable, workingDir: workingDir, state: StateStopped}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.state == StateRunning || m.state == StateStarting || m.state == StateStopping {
		m.mu.Unlock()
		return ErrAlreadyRunning
	}
	m.state = StateStarting
	cmd := exec.Command(m.executable)
	cmd.Dir = m.workingDir
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+m.workingDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return err
	}
	now := time.Now().UTC()
	res, err := m.db.ExecContext(ctx, `INSERT INTO server_runs(server_id,state,started_at,created_at) VALUES(?,?,?,?)`, m.serverID, StateStarting, now, now)
	if err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return err
	}
	runID, _ := res.LastInsertId()
	if err = cmd.Start(); err != nil {
		m.state = StateStopped
		_, _ = m.db.Exec(`UPDATE server_runs SET state=?,stopped_at=?,error_message=? WHERE id=?`, StateCrashed, time.Now().UTC(), err.Error(), runID)
		m.mu.Unlock()
		return err
	}
	m.cmd = cmd
	m.stdin = stdin
	m.runID = runID
	m.startedAt = now
	m.done = make(chan struct{})
	m.stopping = false
	m.state = StateRunning
	_, _ = m.db.Exec(`UPDATE server_runs SET state=?,pid=? WHERE id=?`, StateRunning, cmd.Process.Pid, runID)
	m.mu.Unlock()
	go m.readPipe(stdout, "stdout", runID)
	go m.readPipe(stderr, "stderr", runID)
	go m.wait(cmd, runID)
	m.logs.Publish(&runID, "panel", "Minecraft server started")
	_ = m.audit.Record(ctx, "user", "local", "server.start", m.serverID, fmt.Sprintf("pid=%d", cmd.Process.Pid))
	slog.Info("server started", "pid", cmd.Process.Pid, "run_id", runID)
	return nil
}

func (m *Manager) readPipe(r io.Reader, source string, runID int64) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		m.logs.Publish(&runID, source, s.Text())
	}
}

func (m *Manager) wait(cmd *exec.Cmd, runID int64) {
	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
	}
	m.mu.Lock()
	stopping := m.stopping
	if stopping {
		m.state = StateStopped
	} else {
		m.state = StateCrashed
		m.crashes.Add(1)
	}
	state := m.state
	m.cmd = nil
	m.stdin = nil
	done := m.done
	m.mu.Unlock()
	_, _ = m.db.Exec(`UPDATE server_runs SET state=?,stopped_at=?,exit_code=?,error_message=? WHERE id=?`, state, time.Now().UTC(), exitCode, errorText(err), runID)
	close(done)
	if stopping {
		m.logs.Publish(&runID, "panel", "Minecraft server stopped")
	} else {
		m.logs.Publish(&runID, "panel", fmt.Sprintf("Minecraft server exited unexpectedly (code %d)", exitCode))
	}
	slog.Info("server exited", "exit_code", exitCode, "state", state)
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.state != StateRunning {
		m.mu.Unlock()
		return ErrNotRunning
	}
	m.state = StateStopping
	m.stopping = true
	stdin := m.stdin
	cmd := m.cmd
	done := m.done
	runID := m.runID
	m.mu.Unlock()
	if _, err := io.WriteString(stdin, "stop\n"); err != nil {
		slog.Warn("graceful stop command failed", "error", err)
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	_ = m.audit.Record(context.Background(), "user", "local", "server.stop", m.serverID, fmt.Sprintf("run_id=%d", runID))
	return nil
}

func (m *Manager) Restart(ctx context.Context) error {
	if err := m.Stop(ctx); err != nil {
		return err
	}
	m.restarts.Add(1)
	if err := m.Start(ctx); err != nil {
		return err
	}
	return m.audit.Record(ctx, "user", "local", "server.restart", m.serverID, "")
}
func (m *Manager) Kill(ctx context.Context) error {
	m.mu.Lock()
	if m.state != StateRunning && m.state != StateStopping {
		m.mu.Unlock()
		return ErrNotRunning
	}
	m.stopping = true
	cmd := m.cmd
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return ErrNotRunning
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = m.audit.Record(ctx, "user", "local", "server.kill", m.serverID, "")
	return err
}

func (m *Manager) SendCommand(ctx context.Context, command, source, userID string) error {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, "\r\n") {
		return errors.New("command must be one non-empty line")
	}
	m.mu.Lock()
	if m.state != StateRunning {
		m.mu.Unlock()
		return ErrNotRunning
	}
	isStop := strings.EqualFold(command, "stop")
	if isStop {
		m.state = StateStopping
		m.stopping = true
	}
	stdin := m.stdin
	runID := m.runID
	m.mu.Unlock()
	_, err := io.WriteString(stdin, command+"\n")
	if err != nil && isStop {
		m.mu.Lock()
		if m.state == StateStopping && m.cmd != nil {
			m.state = StateRunning
			m.stopping = false
		}
		m.mu.Unlock()
	}
	m.commands.Add(1)
	_, _ = m.db.ExecContext(ctx, `INSERT INTO command_entries(server_id,run_id,command,source,user_id,success,error_message,created_at) VALUES(?,?,?,?,?,?,?,?)`, m.serverID, runID, command, source, userID, err == nil, errorText(err), time.Now().UTC())
	_ = m.audit.Record(ctx, "user", userID, "server.command", m.serverID, command)
	return err
}

func (m *Manager) Status(context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{State: m.state, RunID: m.runID}
	if m.cmd != nil && m.cmd.Process != nil {
		s.PID = m.cmd.Process.Pid
	}
	if !m.startedAt.IsZero() && (m.state == StateRunning || m.state == StateStopping) {
		t := m.startedAt
		s.StartedAt = &t
		s.UptimeSeconds = int64(time.Since(t).Seconds())
	}
	return s, nil
}
func (m *Manager) Counters() (uint64, uint64, uint64) {
	return m.restarts.Load(), m.crashes.Load(), m.commands.Load()
}
func (m *Manager) Shutdown(ctx context.Context) error {
	s, _ := m.Status(ctx)
	if s.State == StateRunning {
		return m.Stop(ctx)
	}
	return nil
}
