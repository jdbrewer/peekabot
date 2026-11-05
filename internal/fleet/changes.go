package fleet

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RepoChanges is what one session changed in one git repo.
type RepoChanges struct {
	Root    string       // git toplevel; "" for files outside any repo
	Files   []FileChange // in the order the session first wrote them
	Commits []string     // "abc1234 subject", newest first: commits since the session started that touch Files
	Diff    string       // coloured patch against HEAD, untracked files included
	Err     string
}

// FileChange is one written file compared with HEAD.
type FileChange struct {
	Path           string // relative to the repo root (absolute outside a repo)
	Status         string // modified, new, deleted, or same (no diff against HEAD: committed or undone)
	Added, Removed int
	Binary         bool
}

// emptyTree is git's well-known empty tree, the base for a repo with no commits yet.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// maxDiffLines keeps a generated file from turning the review into megabytes.
const maxDiffLines = 3000

// Review gathers what session s changed: every file it wrote, grouped by the repo each
// file lives in (worktrees and multi-repo sessions included), compared with HEAD.
func Review(s Session) []RepoChanges {
	type group struct {
		root  string
		files []string // repo-relative
	}
	var groups []*group
	byRoot := map[string]*group{}
	var outside []FileChange
	for _, p := range s.Info.Written {
		root, rel := repoPath(p)
		if root == "" {
			outside = append(outside, FileChange{Path: p, Status: fileStatus(p)})
			continue
		}
		g := byRoot[root]
		if g == nil {
			g = &group{root: root}
			byRoot[root] = g
			groups = append(groups, g)
		}
		g.files = append(g.files, rel)
	}
	var out []RepoChanges
	for _, g := range groups {
		out = append(out, reviewRepo(g.root, g.files, s))
	}
	if len(outside) > 0 {
		out = append(out, RepoChanges{Files: outside})
	}
	return out
}

// repoPath finds the git toplevel holding p and p's path inside it. --show-prefix is
// used instead of filepath.Rel because the toplevel comes back with symlinks resolved
// (/tmp is /private/tmp on macOS) while transcript paths don't; the root returned is
// spelled the way p spells it.
func repoPath(p string) (root, rel string) {
	dir := filepath.Dir(p)
	for dir != "/" && dir != "." {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			break
		}
		dir = filepath.Dir(dir) // the file's directory was deleted too
	}
	out, err := gitOut(dir, "rev-parse", "--show-toplevel", "--show-prefix")
	if err != nil {
		return "", ""
	}
	lines := strings.SplitN(strings.TrimRight(out, "\n"), "\n", 2)
	if len(lines) < 2 {
		lines = append(lines, "")
	}
	inDir, _ := filepath.Rel(dir, p)
	rel = filepath.ToSlash(filepath.Join(lines[1], inDir))
	root = lines[0]
	if strings.HasSuffix(p, "/"+rel) { // keep the root as the session spells it, not symlink-resolved
		root = strings.TrimSuffix(p, "/"+rel)
	}
	return root, rel
}

func reviewRepo(root string, files []string, s Session) RepoChanges {
	rc := RepoChanges{Root: root}
	base := "HEAD"
	if _, err := gitOut(root, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		base = emptyTree
	}
	args := append([]string{"--"}, files...)

	numstat, err := gitOut(root, append([]string{"diff", "--numstat", "-z", base}, args...)...)
	if err != nil {
		rc.Err = err.Error()
	}
	counts := parseNumstat(numstat)
	status, _ := gitOut(root, append([]string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}, args...)...)
	codes := parsePorcelain(status)

	var untracked []string
	for _, f := range files {
		fc := FileChange{Path: f, Status: "same"}
		c, changed := counts[f]
		switch {
		case codes[f] == "??":
			fc.Status = "new"
			fc.Added, fc.Binary = countLines(filepath.Join(root, f))
			untracked = append(untracked, f)
		case changed && !exists(filepath.Join(root, f)):
			fc.Status, fc.Removed, fc.Binary = "deleted", c.removed, c.binary
		case changed && (base == emptyTree || strings.HasPrefix(codes[f], "A")):
			fc.Status, fc.Added, fc.Binary = "new", c.added, c.binary
		case changed:
			fc.Status, fc.Added, fc.Removed, fc.Binary = "modified", c.added, c.removed, c.binary
		case !exists(filepath.Join(root, f)):
			fc.Status = "deleted" // never committed, and gone again
		}
		rc.Files = append(rc.Files, fc)
	}

	if base == "HEAD" && !s.Started.IsZero() {
		log, _ := gitOut(root, append([]string{"log", "-20", "--format=%h %s",
			"--since=" + strconv.FormatInt(s.Started.Unix(), 10)}, args...)...)
		for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
			if l != "" {
				rc.Commits = append(rc.Commits, l)
			}
		}
	}

	diff, _ := gitOut(root, append([]string{"diff", "--color=always", base}, args...)...)
	var b strings.Builder
	b.WriteString(diff)
	for _, f := range untracked {
		// --no-index exits 1 when the files differ, which is the point
		d, _ := gitOut(root, "diff", "--color=always", "--no-index", "--", "/dev/null", f)
		b.WriteString(d)
	}
	rc.Diff = capLines(b.String(), maxDiffLines)
	return rc
}

type numstat struct {
	added, removed int
	binary         bool
}

// parseNumstat reads `git diff --numstat -z`: "added\tremoved\tpath\0" per file, with
// "-\t-\t" for binary files.
func parseNumstat(out string) map[string]numstat {
	m := map[string]numstat{}
	for _, rec := range strings.Split(out, "\x00") {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) != 3 {
			continue
		}
		a, errA := strconv.Atoi(f[0])
		r, errR := strconv.Atoi(f[1])
		m[f[2]] = numstat{a, r, errA != nil || errR != nil}
	}
	return m
}

// parsePorcelain reads `git status --porcelain=v1 -z`: "XY path\0" per entry.
func parsePorcelain(out string) map[string]string {
	m := map[string]string{}
	for _, rec := range strings.Split(out, "\x00") {
		if len(rec) > 3 {
			m[rec[3:]] = rec[:2]
		}
	}
	return m
}

func countLines(p string) (n int, binary bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, false
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return 0, true
	}
	n = bytes.Count(b, []byte("\n"))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n, false
}

func fileStatus(p string) string {
	if exists(p) {
		return "same"
	}
	return "deleted"
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func capLines(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "") + "\x1b[0m… " + strconv.Itoa(len(lines)-n) + " more lines\n"
}

// gitOut runs git in dir without taking optional locks: an agent may be running git in
// the same repo, and a plain `git status` would grab index.lock to refresh the index.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return string(out), err
}
