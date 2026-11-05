package fleet

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func screen(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParsePrompt(t *testing.T) {
	cases := []struct {
		file    string
		ok      bool
		yes, no int
		shows   *Call
	}{
		// Bash puts No at 4, after an "auto mode" option that must never be pressed by number
		{"prompt-bash.txt", true, 1, 4, newCall("x", "Bash", []byte(`{"command":"touch probe-a.txt"}`))},
		{"prompt-write.txt", true, 1, 3, newCall("x", "Write", []byte(`{"file_path":"/tmp/probe/notes.txt","content":"hello"}`))},
		{"prompt-question.txt", false, 0, 0, nil}, // AskUserQuestion: options, but not a permission
		{"prompt-trust.txt", false, 0, 0, nil},    // folder trust: default is "No, exit"
	}
	for _, c := range cases {
		p, ok := ParsePrompt(screen(t, c.file))
		if ok != c.ok || p.Yes != c.yes || p.No != c.no {
			t.Errorf("%s: ok=%v yes=%d no=%d, want %v %d %d", c.file, ok, p.Yes, p.No, c.ok, c.yes, c.no)
		}
		if c.shows != nil && !p.Shows(c.shows) {
			t.Errorf("%s: dialog doesn't show %+v", c.file, c.shows)
		}
	}
}

func TestParsePromptIgnoresHistory(t *testing.T) {
	s := screen(t, "prompt-bash.txt") + "\n⏺ Bash(touch probe-a.txt)\n  ⎿  Done\n\n❯ \n"
	if _, ok := ParsePrompt(s); ok {
		t.Error("a dialog pushed up by later output was read as live")
	}
}

func TestPromptShowsOnlyItsCall(t *testing.T) {
	p, _ := ParsePrompt(screen(t, "prompt-bash.txt"))
	for _, c := range []*Call{
		newCall("x", "Bash", []byte(`{"command":"rm -rf build"}`)),
		newCall("x", "mcp__db__query", []byte(`{"query":"select 1"}`)), // nothing reliable to compare
		nil,
	} {
		if p.Shows(c) {
			t.Errorf("bash dialog claims to show %+v", c)
		}
	}
	// a long command wraps in the dialog; only a prefix is compared, whitespace ignored
	long := "pnpm --filter @acme/api run migrate:latest --env staging --verbose"
	wrapped := strings.Replace(screen(t, "prompt-bash.txt"), " touch probe-a.txt\n", " pnpm --filter @acme/api run mig\n rate:latest --env staging --verbose\n", 1)
	p, _ = ParsePrompt(wrapped)
	if !p.Shows(newCall("x", "Bash", []byte(`{"command":"`+long+`"}`))) {
		t.Error("wrapped long command not matched")
	}
}

func TestPendingCall(t *testing.T) {
	tail := newTranscriptTail("", false)
	tail.consume([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash","input":{"command":"make test\nmake lint","description":"Run tests"}},{"type":"tool_use","id":"b","name":"Edit","input":{"file_path":"/r/x.go"}}]}}`))
	if p := tail.info.Pending; p == nil || p.ID != "b" || p.Summary != "/r/x.go" {
		t.Fatalf("pending = %+v, want the newest open call b", p)
	}
	tail.consume([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":"ok"}]}}`))
	if p := tail.info.Pending; p == nil || p.ID != "a" || p.Summary != "make test" {
		t.Fatalf("pending = %+v, want a (command, not description)", p)
	}
	tail.consume([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"ok"}]}}`))
	if tail.info.Pending != nil || tail.info.Tool != "" {
		t.Fatalf("pending = %+v after every result", tail.info.Pending)
	}
	tail.consume([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"q","name":"AskUserQuestion","input":{"questions":[{"question":"Red or blue?","options":[]}]}}]}}`))
	if p := tail.info.Pending; p == nil || p.Summary != "Red or blue?" {
		t.Fatalf("question pending = %+v", p)
	}
}

// answerFleet is one waiting session whose pane shows screenText; keys sent are recorded.
func answerFleet(t *testing.T, waitingFor, screenText string) (*Fleet, *[]string) {
	t.Helper()
	claude := t.TempDir()
	at := strconv.FormatInt(time.Now().Add(-time.Minute).UnixMilli(), 10)
	write(t, filepath.Join(claude, "sessions"), "1.json",
		`{"pid":1,"sessionId":"s1","name":"one","cwd":"/nowhere","status":"waiting","waitingFor":"`+waitingFor+`","statusUpdatedAt":`+at+`,"tmux":"w:@1.%5"}`)
	write(t, filepath.Join(claude, "projects", "-nowhere"), "s1.jsonl",
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"touch probe-a.txt"}}]}}`+"\n")
	f := New(Config{ClaudeDir: claude, StateFile: filepath.Join(t.TempDir(), "seen.json")})
	f.alive = func(rs []rawSession) map[int]bool { return map[int]bool{1: true} }
	f.panes = func() map[string]Pane { return map[string]Pane{"%5": {ID: "%5"}} }
	f.screen = func(string) string { return screenText }
	var sent []string
	f.send = func(pane, key string) error { sent = append(sent, pane+" "+key); return nil }
	return f, &sent
}

func TestAnswer(t *testing.T) {
	bash := screen(t, "prompt-bash.txt")
	f, sent := answerFleet(t, WaitingPermission, bash)
	ss, _ := f.Snapshot()
	if !ss[0].Answerable() || ss[0].Info.Pending.Summary != "touch probe-a.txt" {
		t.Fatalf("session not answerable: %+v", ss[0])
	}
	if err := f.Answer("s1", "t1", true); err != nil {
		t.Fatal(err)
	}
	if err := f.Answer("s1", "t1", false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*sent, ",") != "%5 1,%5 4" {
		t.Errorf("sent %q, want Yes=1 then No=4", *sent)
	}

	refuse := []struct {
		name               string
		waitingFor, screen string
		callID             string
	}{
		{"a question", "input needed", bash, "t1"},
		{"a different call than the board showed", WaitingPermission, bash, "t0"},
		{"no dialog on screen", WaitingPermission, "❯ \n", "t1"},
		{"a dialog for another command", WaitingPermission, strings.Replace(bash, "touch probe-a.txt", "rm -rf /", -1), "t1"},
	}
	for _, r := range refuse {
		f, sent := answerFleet(t, r.waitingFor, r.screen)
		if err := f.Answer("s1", r.callID, true); err == nil || len(*sent) > 0 {
			t.Errorf("%s: err=%v sent=%q; want refusal and no keys", r.name, err, *sent)
		}
	}
}
