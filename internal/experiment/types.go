// Package experiment owns explicitly configured benchmark child processes:
// the ARIES CLI or an AgentSysBench deployment driver.
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
	ErrNotReady = errors.New("experiment execution is not ready")
	ErrNotFound = errors.New("experiment or profile not found")
	ErrInvalid  = errors.New("invalid experiment request")
)

type Config struct {
	Backend                                  string // "aries" (default) or "agentsysbench"
	Root, Binary, Executor, Machine, JobsDir string
	StopTimeout                              time.Duration
	OnArtifacts                              func(string, string) (string, error)
}
type Preset struct {
	ModelSettings   ModelSettings `json:"model_settings"`
	ModelEditable   bool          `json:"model_editable"`
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Workload        string        `json:"workload"`
	Harness         string        `json:"harness"`
	Model           string        `json:"model"`
	Backend         string        `json:"backend"`
	Tasks           []string      `json:"tasks"`
	Concurrency     int           `json:"concurrency"`
	LoopDuration    string        `json:"loop_duration,omitempty"`
	ArrivalSchedule bool          `json:"arrival_schedule"`
	// AgentSysBench: the loadgen sends the first N manifest requests, so a
	// request count replaces task selection.
	RequestCount     bool   `json:"request_count,omitempty"`
	ConcurrencyFixed bool   `json:"concurrency_fixed,omitempty"`
	Hardware         string `json:"hardware,omitempty"`
}
type Catalog struct {
	Backend  string   `json:"backend"`
	Enabled  bool     `json:"enabled"`
	Ready    bool     `json:"ready"`
	Executor string   `json:"executor"`
	Machine  string   `json:"machine,omitempty"`
	Message  string   `json:"message,omitempty"`
	Profiles []Preset `json:"profiles"`
}
type Request struct {
	Model             *ModelSettings `json:"model,omitempty"`
	ProfileID         string         `json:"profile_id"`
	Action            string         `json:"action"`
	Tasks             []string       `json:"tasks,omitempty"`
	Concurrency       int            `json:"concurrency,omitempty"`
	RequestNum        int            `json:"request_num,omitempty"`
	ConfirmModelCalls bool           `json:"confirm_model_calls"`
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
	RequestNum       int        `json:"request_num,omitempty"`
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
