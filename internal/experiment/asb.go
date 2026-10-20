package experiment

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aeilot/TraceX/adapters/agentsysbench"
)

const (
	BackendARIES = "aries"
	BackendASB   = "agentsysbench"
)

// asbSetting describes one AgentSysBench deployment. Variable names are fixed
// here (and only exported as data), never taken from requests.
type asbSetting struct {
	ID, Scenario, Agent string
	AgentDirs           []string // agents/<dir> images built by Prepare
	RequestVar          string
	ConcurrencyVar      string // empty: concurrency is fixed by the deployment
	Hardware            string // empty: CPU only
}

// From benchmark/deployment/*/docker-compose.yaml at AgentSysBench d653ab2.
var asbSettings = []asbSetting{
	{"qa-rag", "QA", "RAG", []string{"rag"}, "RAG_REQUEST_NUM", "", ""},
	{"multimodal-hugginggpt", "MultiModal", "HuggingGPT", []string{"hugginggpt"}, "HUGGINGGPT_REQUEST_NUM", "", "3 GPUs"},
	{"ai-search-deep-research", "AISearch", "DeepResearch", []string{"deep-research"}, "REQUEST_NUM", "", ""},
	{"swe-mini-swe-agent", "SWE", "MiniSWE", []string{"mini-swe-agent"}, "MINI_SWEAGENT_REQUEST_NUM", "MINI_SWEAGENT_CONCURRENCY", "x86_64"},
	{"terminal-use-codex", "TerminalUse", "Codex", []string{"codex"}, "CODEX_REQUEST_NUM", "CODEX_CONCURRENCY", ""},
	{"browser-use-webagent", "BrowserUse", "WebAgent", []string{"webagent"}, "WEBAGENT_REQUEST_NUM", "WEBAGENT_CONCURRENCY", ""},
	{"computer-use-gui-agent", "ComputerUse", "GUIAgent", []string{"gui-agent"}, "OS_WORLD_REQUEST_NUM", "OS_WORLD_CONCURRENCY", "KVM"},
	{"tool-rich-assistant-claude", "ToolRichAssistant", "Claude", []string{"claude"}, "CLAUDE_REQUEST_NUM", "CLAUDE_CONCURRENCY", ""},
	{"office-work-openclaw", "OfficeWork", "OpenClaw", []string{"openclaw"}, "OPENCLAW_REQUEST_NUM", "OPENCLAW_CONCURRENCY", ""},
	{"auto-research-pi-autoresearch", "AutoResearch", "PiAutoResearch", []string{"pi-autoresearch"}, "PI_AUTORESEARCH_REQUEST_NUM", "PI_AUTORESEARCH_CONCURRENCY", "1 GPU"},
}

func asbLookup(id string) (asbSetting, bool) {
	for _, s := range asbSettings {
		if s.ID == id {
			return s, true
		}
	}
	return asbSetting{}, false
}

func isASBRoot(root string) bool {
	info, err := os.Stat(filepath.Join(root, "benchmark", "deployment"))
	return err == nil && info.IsDir()
}

func asbCatalog(root string) []Preset {
	out := []Preset{}
	for _, s := range asbSettings {
		if info, err := os.Stat(filepath.Join(root, "benchmark", "deployment", s.ID, "scripts")); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, Preset{ID: s.ID, Name: s.Scenario + " × " + s.Agent, Workload: s.Scenario, Harness: s.Agent,
			Model: asbModel(root, s.AgentDirs[0]), Backend: BackendASB, Tasks: []string{}, Concurrency: 1,
			RequestCount: true, ConcurrencyFixed: s.ConcurrencyVar == "", Hardware: s.Hardware})
	}
	return out
}

var modelLine = regexp.MustCompile(`^([A-Z][A-Z0-9_]*MODEL)=["']?([^"'#\s]{1,128})`)

// asbModel reads the model name configure-api.sh wrote for an agent. Only a
// *_MODEL value is read; keys and endpoints in the same file are skipped.
func asbModel(root, agent string) string {
	f, err := os.Open(filepath.Join(root, "agents", agent, "config", ".env"))
	if err != nil {
		return ""
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if m := modelLine.FindStringSubmatch(strings.TrimSpace(scan.Text())); m != nil && !strings.Contains(m[1], "KEY") {
			return m[2]
		}
	}
	return ""
}

// startASB writes the embedded driver into a private job directory and starts
// it. The caller holds m.mu and has validated the action and acknowledgment.
func (m *Manager) startASB(req Request, p *Preset) (Job, error) {
	s, ok := asbLookup(p.ID)
	if !ok {
		return Job{}, ErrNotFound
	}
	n, concurrency := req.RequestNum, req.Concurrency
	if req.Action == "run" && (n < 1 || n > 100000) {
		return Job{}, fmt.Errorf("%w: request count must be between 1 and 100000", ErrInvalid)
	}
	if concurrency < 0 || concurrency > 4096 {
		return Job{}, fmt.Errorf("%w: concurrency must be between 1 and 4096", ErrInvalid)
	}
	if concurrency == 0 || s.ConcurrencyVar == "" {
		concurrency = 1
	}
	dir, artifacts, id, err := m.jobDir()
	if err != nil {
		return Job{}, err
	}
	for _, name := range agentsysbench.Names {
		b, e := agentsysbench.Files.ReadFile(name)
		if e != nil {
			return Job{}, e
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
			return Job{}, e
		}
	}
	runID := fmt.Sprintf("%s-%s-n%d-c%d", time.Now().UTC().Format("20060102T150405Z"), s.ID, n, concurrency)
	args := []string{"/bin/sh", filepath.Join(dir, "driver.sh"), req.Action, m.cfg.Root, s.ID, artifacts, strings.Join(s.AgentDirs, " ")}
	if req.Action == "run" {
		args = append(args, s.RequestVar, fmt.Sprint(n), s.ConcurrencyVar, fmt.Sprint(concurrency), dir, runID)
	} else {
		n = 0
	}
	job := Job{ID: id, Action: req.Action, ProfileID: p.ID, Name: p.Name, Workload: p.Workload, Harness: p.Harness,
		Model: p.Model, Backend: BackendASB, Tasks: []string{}, RequestNum: n, Concurrency: concurrency}
	// The artifacts path is unique to this job; cancellation matches it in the live cmdline.
	return m.launch(job, dir, artifacts, args)
}

// asbResult reads outcome enums from the driver's markers and the final profile.
func asbResult(job *Job, dir string) {
	marker := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return strings.TrimSpace(string(b))
	}
	switch marker("cleanup.status") {
	case "succeeded":
		job.CleanupStatus = "confirmed"
	default:
		job.CleanupStatus = "unconfirmed"
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "artifacts"))
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(dir, "artifacts", entry.Name(), "profile.json"))
		if err != nil || len(b) > 256<<20 {
			continue
		}
		var prof struct {
			Tasks []struct {
				Status           string `json:"status"`
				EvaluationStatus string `json:"evaluation_status"`
			} `json:"tasks"`
		}
		if json.Unmarshal(b, &prof) != nil {
			continue
		}
		harness, evaluation := "", ""
		for _, t := range prof.Tasks {
			harness = aggregateOutcome(harness, outcome(t.Status))
			e := t.EvaluationStatus
			if e == "passed" {
				e = "succeeded"
			}
			evaluation = aggregateOutcome(evaluation, outcome(e))
		}
		if harness == "" {
			harness = "not_run"
		}
		if evaluation == "" {
			evaluation = "not_run"
		}
		job.HarnessStatus, job.EvaluationStatus = harness, evaluation
		return
	}
}
