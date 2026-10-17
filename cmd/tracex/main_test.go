package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	var out, stderr bytes.Buffer
	if err := run(context.Background(), []string{"--export", path}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) {
		t.Fatalf("export: %v, %s", err, data)
	}
	out.Reset()
	if err := run(context.Background(), []string{"--run", path, "--export", "-"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("stdout must contain JSON only: %s", out.String())
	}
}

func TestCLIErrorAndHelp(t *testing.T) {
	var out, stderr bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "--run") {
		t.Fatal("missing run usage")
	}
	if err := run(context.Background(), []string{"unexpected"}, &out, &stderr); err == nil {
		t.Fatal("positional arguments must fail")
	}
	if err := run(context.Background(), []string{"--run", filepath.Join(t.TempDir(), "missing")}, &out, &stderr); err == nil {
		t.Fatal("missing input must fail")
	}
}

func TestGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"--addr", "127.0.0.1:0"}, &bytes.Buffer{}, &bytes.Buffer{}) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
}

func TestOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	err = run(context.Background(), []string{"--addr", listener.Addr().String()}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "choose another port") {
		t.Fatalf("expected actionable listen error, got %v", err)
	}
}

func TestMultipleSourcesExportRejected(t *testing.T) {
	var out, stderr bytes.Buffer
	err := run(context.Background(), []string{"--run", "a", "--run", "b", "--export", "-"}, &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "single") {
		t.Fatalf("expected single-source restriction: %v", err)
	}
}
func TestDiscoveryErrors(t *testing.T) {
	var out, stderr bytes.Buffer
	for _, args := range [][]string{{"--runs-dir", t.TempDir()}, {"--watch", "-1s"}, {"--run", ""}} {
		if e := run(context.Background(), args, &out, &stderr); e == nil {
			t.Fatalf("expected error: %v", args)
		}
	}
}

func TestExperimentFlagValidation(t *testing.T) {
	for _, args := range [][]string{
		{"--aries-dir", ".", "--export", "-"},
		{"--aries-executor", "remote"},
		{"--aries-dir", ".", "--aries-executor", "orb"},
		{"--aries-bin", "aries"},
		{"--aries-dir", ".", "--aries-machine", "vm"},
		{"--stop-timeout", "0s"},
	} {
		if err := run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("expected flag validation: %v", args)
		}
	}
}
