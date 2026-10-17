// Package experiment owns explicitly configured ARIES child processes.
package experiment

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

var (
	ErrBusy     = errors.New("an experiment is already active")
	ErrNotReady = errors.New("ARIES execution is not ready")
	ErrNotFound = errors.New("experiment or profile not found")
	ErrInvalid  = errors.New("invalid experiment request")
)

type Config struct {
	Root, Binary, Executor, Machine, JobsDir string
	StopTimeout                              time.Duration
	OnArtifacts                              func(string, string) (string, error)
}
type Preset struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Workload        string   `json:"workload"`
	Harness         string   `json:"harness"`
	Model           string   `json:"model"`
	Backend         string   `json:"backend"`
	Tasks           []string `json:"tasks"`
	Concurrency     int      `json:"concurrency"`
	LoopDuration    string   `json:"loop_duration,omitempty"`
	ArrivalSchedule bool     `json:"arrival_schedule"`
}
type Catalog struct {
	Enabled  bool     `json:"enabled"`
	Ready    bool     `json:"ready"`
	Executor string   `json:"executor"`
	Machine  string   `json:"machine,omitempty"`
	Message  string   `json:"message,omitempty"`
	Profiles []Preset `json:"profiles"`
}
type Request struct {
	ProfileID         string   `json:"profile_id"`
	Action            string   `json:"action"`
	Tasks             []string `json:"tasks,omitempty"`
	Concurrency       int      `json:"concurrency,omitempty"`
	ConfirmModelCalls bool     `json:"confirm_model_calls"`
}
type Job struct {
	ID               string     `json:"id"`
	Action           string     `json:"action"`
	ProfileID        string     `json:"profile_id"`
	Name             string     `json:"name"`
	Workload         string     `json:"workload"`
	Harness          string     `json:"harness"`
	Model            string     `json:"model"`
	Backend          string     `json:"backend"`
	Tasks            []string   `json:"tasks"`
	Concurrency      int        `json:"concurrency"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	ExitCode         *int       `json:"exit_code,omitempty"`
	RunID            string     `json:"run_id,omitempty"`
	CleanupStatus    string     `json:"cleanup_status"`
	HarnessStatus    string     `json:"harness_status,omitempty"`
	EvaluationStatus string     `json:"evaluation_status,omitempty"`
	Error            string     `json:"error,omitempty"`
}
type record struct {
	job           Job
	cmd           *exec.Cmd
	dir, profile  string
	done          chan struct{}
	attached      string
	stopRequested bool
}
type Manager struct {
	mu      sync.Mutex
	cfg     Config
	catalog Catalog
	jobs    []*record
	active  *record
	closed  bool
	cancel  context.CancelFunc
}
