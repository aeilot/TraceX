package profile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderDecodesOnlyAppendedNativeRecordsAndResets(t *testing.T) {
	root := t.TempDir()
	path := "task/monitor/resources.jsonl"
	first := "{\"time\":\"2026-01-01T00:00:01Z\",\"memory_usage_bytes\":10}\n"
	write(t, root, path, first)
	l := NewLoader(root)
	p, err := l.Load()
	if err != nil || len(p.Resources) != 1 {
		t.Fatalf("initial %v %d", err, len(p.Resources))
	}
	n := l.cache.decoded
	if _, err = l.Load(); err != nil {
		t.Fatal(err)
	}
	if l.cache.decoded != n {
		t.Fatal("unchanged file reparsed")
	}
	f, err := os.OpenFile(filepath.Join(root, path), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{\"time\":\"2026-01-01T00:00:02Z\",")
	f.Close()
	p, err = l.Load()
	if err != nil || len(p.Resources) != 1 {
		t.Fatal("partial tail corrupted snapshot")
	}
	n = l.cache.decoded
	f, _ = os.OpenFile(filepath.Join(root, path), os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString("\"memory_usage_bytes\":20}\n")
	f.Close()
	p, err = l.Load()
	if err != nil || len(p.Resources) != 2 {
		t.Fatalf("append %v %d", err, len(p.Resources))
	}
	if l.cache.decoded != n+1 {
		t.Fatal("complete prefix decoded again")
	}
	if err = os.Remove(filepath.Join(root, path)); err != nil {
		t.Fatal(err)
	}
	write(t, root, path, "{\"time\":\"2026-01-01T00:00:03Z\",\"memory_usage_bytes\":30}\n")
	p, err = l.Load()
	if err != nil || len(p.Resources) != 1 || *p.Resources[0].MemoryBytes != 30 {
		t.Fatal("rotation retained stale data")
	}
	write(t, root, path, "")
	p, err = l.Load()
	if err != nil || len(p.Resources) != 0 {
		t.Fatal("truncate retained stale data")
	}
}

func TestRecordCacheGlobalBoundAndUnchangedAccounting(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.jsonl", "{}\n{}\n")
	write(t, root, "b.jsonl", "{}\n{}\n")
	c := recordCache{limit: 3}
	consume := func(map[string]any, int) {}
	if _, _, e := c.read(filepath.Join(root, "a.jsonl"), consume); e != nil {
		t.Fatal(e)
	}
	if _, _, e := c.read(filepath.Join(root, "a.jsonl"), consume); e != nil || c.retained != 2 {
		t.Fatal("unchanged records recounted", e, c.retained)
	}
	if _, _, e := c.read(filepath.Join(root, "b.jsonl"), consume); !errors.Is(e, errCachedRecordLimit) {
		t.Fatal("global irrelevant-map bound missing", e)
	}
	if c.retained != 3 {
		t.Fatal("retained count exceeded bound", c.retained)
	}
	if _, _, e := c.read(filepath.Join(root, "b.jsonl"), consume); !errors.Is(e, errCachedRecordLimit) {
		t.Fatal("failed file became valid partial snapshot", e)
	}
}
func TestRecordCacheTailRotationTruncateRemovalReleaseBudget(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a.jsonl"), filepath.Join(root, "b.jsonl")
	consume := func(map[string]any, int) {}
	write(t, root, "a.jsonl", "{}\n{}")
	c := recordCache{limit: 3}
	if _, _, e := c.read(a, consume); e != nil || c.retained != 2 {
		t.Fatal("tail not counted", e, c.retained)
	}
	f, e := os.OpenFile(a, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteString("\n")
	f.Close()
	if _, _, e = c.read(a, consume); e != nil || c.retained != 2 {
		t.Fatal("completed tail counted twice", e, c.retained)
	}
	_ = os.Remove(a)
	write(t, root, "a.jsonl", "{}\n")
	if _, _, e = c.read(a, consume); e != nil || c.retained != 1 {
		t.Fatal("rotation failed release", e, c.retained)
	}
	write(t, root, "a.jsonl", "")
	if _, _, e = c.read(a, consume); e != nil || c.retained != 0 {
		t.Fatal("truncate failed release", e, c.retained)
	}
	write(t, root, "b.jsonl", "{}\n{}\n{}\n")
	if _, _, e = c.read(b, consume); e != nil || c.retained != 3 {
		t.Fatal(e, c.retained)
	}
	_ = os.Remove(b)
	c.pruneStale()
	if c.retained != 0 || c.files[b] != nil {
		t.Fatal("removed file retained records", c.retained)
	}
}
func TestLoaderCacheLimitRejectsSnapshot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "task/monitor/resources.jsonl", "{}\n{}\n{}\n{}\n")
	l := NewLoader(root)
	l.cache.limit = 3
	if _, e := l.Load(); !errors.Is(e, errCachedRecordLimit) {
		t.Fatal("cache limit silently published partial evidence", e)
	}
	write(t, root, "task/monitor/resources.jsonl", "{\"time\":\"2026-01-01T00:00:01Z\",\"memory_usage_bytes\":10}\n")
	p, e := l.Load()
	if e != nil || len(p.Resources) != 1 || l.cache.retained != 1 {
		t.Fatal("recovery after replacement failed", e, l.cache.retained)
	}
}
