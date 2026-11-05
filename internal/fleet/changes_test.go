package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestReview(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null") // no signing or hooks from the machine's config
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")

	repo := t.TempDir()
	git(t, repo, "", "init", "-q")
	write(t, repo, "a.go", "one\ntwo\n")
	write(t, filepath.Join(repo, "sub"), "b.go", "gone\n")
	write(t, repo, "c.go", "old\n")
	git(t, repo, "", "add", ".")
	git(t, repo, "2026-01-01T00:00:00Z", "commit", "-q", "-m", "base")
	// the session starts, then changes things
	write(t, repo, "c.go", "new\n")
	git(t, repo, "", "add", "c.go")
	git(t, repo, "2026-01-02T00:00:00Z", "commit", "-q", "-m", "change c")
	write(t, repo, "a.go", "one\nTWO\n")
	os.Remove(filepath.Join(repo, "sub", "b.go"))
	write(t, repo, "new.go", "x\ny")
	write(t, repo, "added.go", "z\n")
	git(t, repo, "", "add", "added.go")

	fresh := t.TempDir() // a repo with no commits yet
	git(t, fresh, "", "init", "-q")
	write(t, fresh, "first.go", "hello\n")
	nowhere := write(t, t.TempDir(), "notes.txt", "not in git\n")

	s := Session{Started: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Info: TranscriptInfo{Written: []string{
		filepath.Join(repo, "a.go"), filepath.Join(repo, "sub", "b.go"), filepath.Join(repo, "c.go"),
		filepath.Join(repo, "new.go"), filepath.Join(repo, "added.go"), nowhere, filepath.Join(fresh, "first.go"),
	}}}
	rs := Review(s)
	if len(rs) != 3 {
		t.Fatalf("got %d groups, want repo, fresh repo, outside: %+v", len(rs), rs)
	}
	got := map[string]FileChange{}
	for _, f := range rs[0].Files {
		got[f.Path] = f
	}
	want := map[string]FileChange{
		"a.go":     {Path: "a.go", Status: "modified", Added: 1, Removed: 1},
		"sub/b.go": {Path: "sub/b.go", Status: "deleted", Removed: 1},
		"c.go":     {Path: "c.go", Status: "same"}, // committed during the session
		"new.go":   {Path: "new.go", Status: "new", Added: 2},
		"added.go": {Path: "added.go", Status: "new", Added: 1},
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s = %+v, want %+v", p, got[p], w)
		}
	}
	if len(rs[0].Commits) != 1 || !strings.HasSuffix(rs[0].Commits[0], " change c") {
		t.Errorf("commits since the session started = %q, want only 'change c'", rs[0].Commits)
	}
	for _, needle := range []string{"a.go", "TWO", "new.go"} {
		if !strings.Contains(rs[0].Diff, needle) {
			t.Errorf("diff lacks %q", needle)
		}
	}
	if f := rs[1].Files; len(f) != 1 || f[0] != (FileChange{Path: "first.go", Status: "new", Added: 1}) {
		t.Errorf("repo without commits: %+v", rs[1])
	}
	if r := rs[2]; r.Root != "" || len(r.Files) != 1 || r.Files[0].Path != nowhere {
		t.Errorf("file outside git: %+v", r)
	}
}

func TestWrittenTracksTheWholeSession(t *testing.T) {
	tail := newTranscriptTail("", false)
	for _, l := range []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"1","name":"Write","input":{"file_path":"/r/a.go"}}]}}`,
		`{"type":"user","message":{"content":"next"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"2","name":"Edit","input":{"file_path":"/r/b.go"}},{"type":"tool_use","id":"3","name":"Edit","input":{"file_path":"/r/a.go"}}]}}`,
	} {
		tail.consume([]byte(l))
	}
	if strings.Join(tail.info.Written, ",") != "/r/a.go,/r/b.go" || tail.info.TurnFiles != 2 {
		t.Errorf("written = %v, turn files = %d", tail.info.Written, tail.info.TurnFiles)
	}
}
