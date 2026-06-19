package server

import "time"

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
)

type Status struct {
	State         State      `json:"state"`
	PID           int        `json:"pid,omitempty"`
	RunID         int64      `json:"run_id,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	UptimeSeconds int64      `json:"uptime_seconds"`
}
