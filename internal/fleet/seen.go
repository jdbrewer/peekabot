package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// seenStore remembers when the user last looked at each session, so "done" can mean
// "finished since you last looked". It is shared by the board and the badge, so save
// merges with what is on disk rather than overwriting it.
type seenStore struct {
	path    string
	marks   map[string]int64 // unix ms
	dirty   bool
	existed bool
}

// marks closer together than this are not worth a disk write
const seenResolution = 5 * time.Second

// legacy, if set, is read when path doesn't exist yet: the first save then writes path,
// so marks carry over from ccfleet without every idle session turning "done".
func loadSeen(path, legacy string) *seenStore {
	s := &seenStore{path: path, marks: map[string]int64{}}
	for _, p := range []string{path, legacy} {
		if p == "" {
			continue
		}
		if b, err := os.ReadFile(p); err == nil {
			s.existed = true
			s.dirty = p != path
			_ = json.Unmarshal(b, &s.marks)
			break
		}
	}
	return s
}

// empty reports whether no seen file existed, i.e. this is the first run.
func (s *seenStore) empty() bool { return !s.existed }

func (s *seenStore) at(id string) time.Time {
	if ms, ok := s.marks[id]; ok {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

func (s *seenStore) mark(id string, t time.Time) {
	ms := t.UnixMilli()
	if ms-s.marks[id] >= seenResolution.Milliseconds() {
		s.dirty = true
	}
	if ms > s.marks[id] {
		s.marks[id] = ms
	}
}

// marks for sessions gone this long are dropped. Age, not absence alone, so one empty
// or failed read of the sessions dir can't reset everything to "done".
const seenRetention = 24 * time.Hour

func (s *seenStore) prune(live map[string]bool, now time.Time) {
	for id, ms := range s.marks {
		if !live[id] && now.Sub(time.UnixMilli(ms)) > seenRetention {
			delete(s.marks, id)
			s.dirty = true
		}
	}
}

func (s *seenStore) save() {
	if !s.dirty && s.existed {
		return
	}
	// merge newer marks written by another peekabot process
	if b, err := os.ReadFile(s.path); err == nil {
		var disk map[string]int64
		if json.Unmarshal(b, &disk) == nil {
			for id, ms := range disk {
				if _, live := s.marks[id]; live && ms > s.marks[id] {
					s.marks[id] = ms
				}
			}
		}
	}
	b, _ := json.Marshal(s.marks)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil && os.Rename(tmp, s.path) == nil {
		s.dirty, s.existed = false, true
	}
}
