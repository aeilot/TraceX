package profile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Loader retains per-file JSONL decode checkpoints between snapshots. Runtime
// objects and normalized profiles are reconstructed so new absolute origins,
// metadata and out-of-order producer completions remain correctly aligned.
// Raw private fields stay within this in-memory reader and never enter Profile.
type Loader struct {
	path  string
	mu    sync.Mutex
	cache recordCache
}

func NewLoader(path string) *Loader {
	return &Loader{path: path, cache: recordCache{files: map[string]*cachedRecords{}}}
}
func (l *Loader) Load() (Profile, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache.pruneStale()
	return load(l.path, &l.cache)
}

type cachedRecord struct {
	value map[string]any
	line  int
}
type cachedRecords struct {
	info        os.FileInfo
	offset      int64
	line        int
	records     []cachedRecord
	tail        *cachedRecord
	invalid     int
	tailInvalid int
	pending     bool
}
type recordCache struct {
	files    map[string]*cachedRecords
	decoded  int
	retained int
	limit    int // Tests may lower this; zero selects the production maxRecords bound.
}

var errCachedRecordLimit = errors.New("cached evidence record limit exceeded")

func (c *recordCache) discard(path string) {
	if entry := c.files[path]; entry != nil {
		c.retained -= len(entry.records)
		if entry.tail != nil {
			c.retained--
		}
		delete(c.files, path)
	}
}
func (c *recordCache) pruneStale() {
	for path, entry := range c.files {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || staleEntry(entry, info) {
			c.discard(path)
		}
	}
}
func staleEntry(entry *cachedRecords, info os.FileInfo) bool {
	return entry.info == nil || !os.SameFile(entry.info, info) || info.Size() < entry.offset || info.Size() < entry.info.Size() || (info.Size() == entry.info.Size() && !info.ModTime().Equal(entry.info.ModTime()))
}
func (c *recordCache) read(path string, consume func(map[string]any, int)) (int, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false, err
	}
	if !info.Mode().IsRegular() {
		return 0, false, fmt.Errorf("not a regular evidence file")
	}
	if info.Size() > maxRunBytes {
		return 0, false, fmt.Errorf("evidence exceeds streaming limit")
	}
	entry := c.files[path]
	if entry == nil || staleEntry(entry, info) {
		c.discard(path)
		if c.files == nil {
			c.files = map[string]*cachedRecords{}
		}
		entry = &cachedRecords{}
		c.files[path] = entry
	}
	changed := entry.info == nil || info.Size() != entry.info.Size() || !info.ModTime().Equal(entry.info.ModTime())
	if changed {
		f, e := os.Open(path)
		if e != nil {
			return 0, false, e
		}
		defer f.Close()
		if _, e = f.Seek(entry.offset, io.SeekStart); e != nil {
			return 0, false, e
		}
		r := bufio.NewReaderSize(io.LimitReader(f, maxRunBytes+1), 64<<10)
		if entry.tail != nil {
			c.retained--
		}
		entry.tail = nil
		entry.tailInvalid = 0
		entry.pending = false
		for {
			b, e := r.ReadBytes('\n')
			if len(b) == 0 && e == io.EOF {
				break
			}
			line := entry.line + 1
			complete := len(b) > 0 && b[len(b)-1] == '\n'
			if len(bytes.TrimSpace(b)) > 0 {
				var v map[string]any
				c.decoded++
				if len(b) > 4<<20 || decode(b, &v) != nil {
					if complete {
						entry.invalid++
					} else if e == io.EOF {
						entry.pending = true
					} else {
						entry.tailInvalid++
					}
				} else {
					limit := c.limit
					if limit <= 0 || limit > maxRecords {
						limit = maxRecords
					}
					if c.retained >= limit {
						return entry.invalid, entry.pending, fmt.Errorf("%w: %d records", errCachedRecordLimit, limit)
					}
					c.retained++
					record := cachedRecord{v, line}
					if complete {
						entry.records = append(entry.records, record)
					} else {
						entry.tail = &record
					}
				}
			}
			if complete {
				entry.offset += int64(len(b))
				entry.line++
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return entry.invalid, entry.pending, e
			}
		}
		entry.info = info
	}
	for _, r := range entry.records {
		consume(r.value, r.line)
	}
	if entry.tail != nil {
		consume(entry.tail.value, entry.tail.line)
	}
	return entry.invalid + entry.tailInvalid, entry.pending, nil
}
