package app

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Addr, DBPath, ServerID, ServerName       string
	RootDir, ExecutablePath, WorkingDir      string
	AdminUsername, AdminPassword             string
	LogRetentionDays, LogMaxRows             int
	SessionTTLHours                          int
	EnablePrometheus, EnableMCP, AuthEnabled bool
}

func LoadConfig() (Config, error) {
	_ = loadEnvFile("config/app.env")
	cwd, _ := os.Getwd()
	projectRoot := cwd
	if filepath.Base(cwd) == "backend" {
		projectRoot = filepath.Dir(cwd)
	}
	root := env("MC_ROOT_DIR", filepath.Join(projectRoot, "bedrock-server"))
	c := Config{
		Addr: env("ADDR", ":8080"), DBPath: env("DB_PATH", filepath.Join(projectRoot, "backend/data/panel.db")),
		ServerID: env("MC_SERVER_ID", "default"), ServerName: env("MC_SERVER_NAME", "Default Bedrock Server"),
		RootDir: root, ExecutablePath: env("MC_EXECUTABLE_PATH", filepath.Join(root, "bedrock_server")),
		WorkingDir: env("MC_WORKING_DIR", root), LogRetentionDays: envInt("LOG_RETENTION_DAYS", 7),
		LogMaxRows: envInt("LOG_MAX_ROWS", 1000000), EnablePrometheus: envBool("ENABLE_PROMETHEUS", true),
		EnableMCP: envBool("ENABLE_MCP", false), AuthEnabled: envBool("AUTH_ENABLED", true),
		AdminUsername: env("ADMIN_USERNAME", "admin"), AdminPassword: env("ADMIN_PASSWORD", ""),
		SessionTTLHours: envInt("SESSION_TTL_HOURS", 24),
	}
	var err error
	for _, p := range []*string{&c.DBPath, &c.RootDir, &c.ExecutablePath, &c.WorkingDir} {
		*p, err = filepath.Abs(*p)
		if err != nil {
			return c, err
		}
	}
	if c.ServerID == "" {
		return c, errors.New("MC_SERVER_ID is required")
	}
	if c.SessionTTLHours < 1 || c.SessionTTLHours > 24*30 {
		return c, errors.New("SESSION_TTL_HOURS must be between 1 and 720")
	}
	if st, err := os.Stat(c.WorkingDir); err != nil || !st.IsDir() {
		return c, fmt.Errorf("MC_WORKING_DIR is invalid: %s", c.WorkingDir)
	}
	if st, err := os.Stat(c.ExecutablePath); err != nil || st.IsDir() {
		return c, fmt.Errorf("MC_EXECUTABLE_PATH is invalid: %s", c.ExecutablePath)
	}
	return c, nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func envInt(k string, d int) int {
	v, err := strconv.Atoi(env(k, ""))
	if err != nil {
		return d
	}
	return v
}
func envBool(k string, d bool) bool {
	v := env(k, "")
	if v == "" {
		return d
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return d
	}
	return b
}
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && os.Getenv(strings.TrimSpace(k)) == "" {
			_ = os.Setenv(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	return s.Err()
}
