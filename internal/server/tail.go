package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aeilot/TraceX/internal/profile"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type eventTail struct {
	path     string
	info     os.FileInfo
	offset   int64
	partial  []byte
	retained []profile.Event
}

func eventPath(path string) string {
	i, e := os.Stat(path)
	if e != nil {
		return ""
	}
	if i.IsDir() {
		return filepath.Join(path, "events.jsonl")
	}
	if strings.HasSuffix(path, ".jsonl") {
		return path
	}
	return ""
}
func newEventTail(path string) *eventTail {
	if path == "" {
		return nil
	}
	f, e := os.Open(path)
	if e != nil {
		return nil
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return nil
	}
	tail := &eventTail{path: path, info: i, offset: i.Size()} // Retain only the unfinished suffix, never reparse preceding evidence.
	data, e := io.ReadAll(io.LimitReader(f, 256*1024*1024+1))
	if e != nil || len(data) > 256*1024*1024 {
		return nil
	}
	j := bytes.LastIndexByte(data, '\n')
	if j >= 0 {
		var decodeErr error
		tail.retained, decodeErr = decodeEventLines(data[:j+1])
		if decodeErr != nil {
			return nil
		}
		tail.partial = append([]byte{}, data[j+1:]...)
	} else {
		tail.partial = data
	}
	return tail
}
func (t *eventTail) read(base profile.Profile) (profile.Profile, bool, error) {
	f, e := os.Open(t.path)
	if e != nil {
		return base, false, e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return base, false, e
	}
	if !os.SameFile(t.info, i) || i.Size() < t.offset || (i.Size() == t.offset && !i.ModTime().Equal(t.info.ModTime())) {
		return base, true, nil
	}
	if _, e = f.Seek(t.offset, io.SeekStart); e != nil {
		return base, false, e
	}
	b, e := io.ReadAll(io.LimitReader(f, 64*1024*1024+1))
	if e != nil {
		return base, false, e
	}
	if len(b) > 64*1024*1024 {
		return base, false, fmt.Errorf("event append exceeds bounded batch")
	}
	combined := append(append([]byte{}, t.partial...), b...)
	j := bytes.LastIndexByte(combined, '\n')
	if len(combined) > 1024*1024 && j < 0 {
		return base, false, fmt.Errorf("event line exceeds limit")
	}
	if j < 0 {
		t.offset += int64(len(b))
		t.partial = combined
		return base, false, nil
	}
	events, e := decodeEventLines(combined[:j+1])
	if e != nil {
		return base, false, e
	}
	retained := append(append([]profile.Event{}, t.retained...), events...)
	if len(retained) > 1000000 {
		return base, false, fmt.Errorf("event stream exceeds record bound")
	}
	p, e := profile.ApplyEventBatch(base, retained)
	if e != nil {
		return base, false, e
	}
	t.retained = retained
	t.offset += int64(len(b))
	t.partial = append([]byte{}, combined[j+1:]...)
	t.info = i
	return p, false, nil
}
func signatureExceptEvents(path string) string {
	if path == "" {
		return ""
	}
	var b strings.Builder
	events := eventPath(path)
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if p == events {
			return nil
		}
		i, e := d.Info()
		if e == nil {
			fmt.Fprintf(&b, "%s:%d:%d;", p, i.Size(), i.ModTime().UnixNano())
		}
		return nil
	})
	return b.String()
}

func decodeEventLines(data []byte) ([]profile.Event, error) {
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 64*1024), 8*1024*1024)
	out := []profile.Event{}
	for s.Scan() {
		if len(bytes.TrimSpace(s.Bytes())) == 0 {
			continue
		}
		var e profile.Event
		d := json.NewDecoder(bytes.NewReader(s.Bytes()))
		d.UseNumber()
		if err := d.Decode(&e); err != nil {
			return nil, err
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return nil, fmt.Errorf("invalid event line")
		}
		out = append(out, e)
	}
	return out, s.Err()
}
