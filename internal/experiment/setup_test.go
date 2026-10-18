package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurePersistsAndProtectsActiveRunner(t *testing.T) {
	c, _ := fixture(t, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := New(ctx, Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	saved := filepath.Join(t.TempDir(), "settings", "runner.json")
	settings := Settings{Root: c.Root, Binary: c.Binary, Executor: "local"}
	if err = m.Configure(settings, saved); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	var restored Settings
	if err = json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, _ := filepath.EvalSymlinks(c.Root)
	if restored.Root != canonicalRoot || restored.Binary != c.Binary || !m.Catalog().Enabled {
		t.Fatalf("%s %+v", b, m.Catalog())
	}
	info, _ := os.Stat(saved)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	before := m.Settings()
	if err = m.Configure(Settings{Root: "relative", Executor: "local"}, saved); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if m.Settings() != before {
		t.Fatal("failed configure changed runner")
	}
	// A failed persistence step must not replace the working runner either.
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0600)
	if err = m.Configure(settings, filepath.Join(blocker, "runner.json")); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if m.Settings() != before {
		t.Fatal("failed save changed runner")
	}
	j, err := m.Start(Request{ProfileID: "nested/test.json", Action: "run", ConfirmModelCalls: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Configure(settings, saved); !errors.Is(err, ErrBusy) {
		t.Fatalf("active reconfigure: %v", err)
	}
	if _, err = m.Stop(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFinished(t, m, j.ID)
}

func TestModelOverridesOnlyPrivateCopy(t *testing.T) {
	c, path := fixture(t, "test")
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), `"backend":"test"`, `"backend":"test","mode":"external"`, 1))
	os.WriteFile(path, b, 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	settings := &ModelSettings{ID: "new-model", BaseURL: "https://example.com/v1", APIKeyEnv: "NEW_KEY", ReasoningEffort: "low"}
	j, err := m.Start(Request{ProfileID: "nested/test.json", Action: "prepare", Model: settings})
	if err != nil {
		t.Fatal(err)
	}
	waitFinished(t, m, j.ID)
	copied, err := readProfile(filepath.Join(c.Root, ".cache", "tracex", "jobs", j.ID, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	model := obj(copied, "model")
	if model["id"] != "new-model" || model["api_key_env"] != "NEW_KEY" || model["api_key"] != nil || j.Model != "new-model" {
		t.Fatalf("override did not apply: %+v", model)
	}
	original, _ := os.ReadFile(path)
	if string(original) != string(b) {
		t.Fatal("original profile changed")
	}
	catalog, _ := json.Marshal(m.Catalog())
	if strings.Contains(string(catalog), "SECRET_TEST_PROVIDER_CREDENTIAL") {
		t.Fatal("catalog exposed credentials")
	}
	settings.BaseURL = "https://user:secret@example.com/v1"
	if _, err = m.Start(Request{ProfileID: "nested/test.json", Action: "prepare", Model: settings}); !errors.Is(err, ErrInvalid) {
		t.Fatal("credentialed endpoint accepted", err)
	}
}

func TestDiscoveryListsMachinesAndReturnsOnlyCredentialPresence(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "profiles"), 0700)
	toolDir := t.TempDir()
	// The fake execution host prints a credential sentinel; probes must discard it.
	script := "#!/bin/sh\nif [ \"$1\" = list ]; then printf 'aries-vm\\ninvalid name\\n'; else printf 'SECRET_PROBE_OUTPUT\\n'; fi\n"
	os.WriteFile(filepath.Join(toolDir, "orb"), []byte(script), 0700)
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ARIES_DIR", root)
	d := Discover(context.Background(), root)
	if !d.OrbAvailable || len(d.Machines) != 1 || d.Machines[0] != "aries-vm" {
		t.Fatalf("%+v", d)
	}
	canonical, _ := filepath.EvalSymlinks(root)
	count := 0
	for _, p := range d.Roots {
		if p == canonical {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("roots were not deduplicated: %+v", d.Roots)
	}
	c, _ := fixture(t, "test")
	c.Executor = "orb"
	c.Machine = "aries-vm"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	b, _ := json.Marshal(m.Check(ctx))
	if strings.Contains(string(b), "SECRET_PROBE_OUTPUT") {
		t.Fatal("check exposed command output")
	}
	if !strings.Contains(string(b), `"name":"docker","ok":true`) {
		t.Fatalf("%s", b)
	}
}
