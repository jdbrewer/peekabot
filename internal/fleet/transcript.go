package fleet

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Edit is one file write seen in a transcript.
type Edit struct {
	Path string
	At   time.Time
}

// TranscriptInfo is what the board shows from a session's transcript.
type TranscriptInfo struct {
	Title       string    `json:"title,omitempty"`
	LastPrompt  string    `json:"lastPrompt,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Model       string    `json:"model,omitempty"`
	Tool        string    `json:"tool,omitempty"`      // tool of the last tool_use, if still open
	Action      string    `json:"action,omitempty"`    // one-line summary of the last tool_use
	LastText    string    `json:"lastText,omitempty"`  // first meaningful line of the last assistant text, markdown stripped
	Failed      string    `json:"failed,omitempty"`    // error kind when the last reply was an API error, e.g. server_error
	TurnFiles   int       `json:"turnFiles,omitempty"` // distinct files written since your last prompt
	Tokens      int       `json:"contextTokens"`       // context size of the last main-thread request
	LastEventAt time.Time `json:"lastEventAt,omitempty"`
	Pending     *Call     `json:"pending,omitempty"` // newest tool_use still waiting for its result
	Edits       []Edit    `json:"-"`
}

// Call is a tool_use that has no result yet: what a permission prompt or question is about.
// A Call is never modified after it is created, so sharing the pointer is safe.
type Call struct {
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Summary string `json:"summary"` // the command, file, URL or question, on one line
	// match is text the permission dialog for this call must show; "" when there is
	// nothing reliable to compare, and then the board won't answer the prompt.
	match string
}

// transcriptTail incrementally reads a .jsonl transcript by byte offset.
type transcriptTail struct {
	path   string
	offset int64
	mtime  time.Time
	size   int64
	info   TranscriptInfo
	open   []*Call         // tool_uses without a result yet, oldest first
	turn   map[string]bool // files written since your last prompt
	index  bool            // collect searchable text into corpus
	corpus corpus
}

const editWindow = 30 * time.Minute

func newTranscriptTail(path string, index bool) *transcriptTail {
	return &transcriptTail{path: path, index: index}
}

// findTranscript locates <sessionId>.jsonl under projectsDir. The directory name is a
// lossy encoding of cwd, so glob instead of reconstructing it.
func findTranscript(projectsDir, sessionID string) string {
	m, _ := filepath.Glob(filepath.Join(projectsDir, "*", sessionID+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// Refresh reads any bytes appended since the last call. Cheap when nothing changed.
func (t *transcriptTail) Refresh(now time.Time) error {
	st, err := os.Stat(t.path)
	if err != nil {
		return err
	}
	if st.ModTime().Equal(t.mtime) && st.Size() == t.size {
		return nil
	}
	if st.Size() < t.offset { // rewritten; start over
		*t = *newTranscriptTail(t.path, t.index)
	}
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // EOF or partial last line: leave it for next time
		}
		t.offset += int64(len(line))
		t.consume(line)
	}
	t.mtime, t.size = st.ModTime(), st.Size()
	t.pruneEdits(now)
	return nil
}

// Line types worth decoding. Everything else (api-request blobs can be megabytes) is skipped.
var interesting = [][]byte{
	[]byte(`"type":"assistant"`),
	[]byte(`"type":"user"`),
	[]byte(`"type":"ai-title"`),
	[]byte(`"type":"last-prompt"`),
}

type rawLine struct {
	Type        string    `json:"type"`
	AITitle     string    `json:"aiTitle"`
	LastPrompt  string    `json:"lastPrompt"`
	GitBranch   string    `json:"gitBranch"`
	IsSidechain bool      `json:"isSidechain"`
	IsMeta      bool      `json:"isMeta"` // injected by the harness, not typed by you
	IsAPIError  bool      `json:"isApiErrorMessage"`
	Error       string    `json:"error"` // kind of API error: server_error, rate_limit, …
	Timestamp   time.Time `json:"timestamp"`
	Message     struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input         int `json:"input_tokens"`
			CacheRead     int `json:"cache_read_input_tokens"`
			CacheCreation int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

func (t *transcriptTail) consume(line []byte) {
	ok := false
	for _, m := range interesting {
		if bytes.Contains(line, m) {
			ok = true
			break
		}
	}
	if !ok {
		return
	}
	var l rawLine
	if json.Unmarshal(line, &l) != nil {
		return
	}
	in := &t.info
	switch l.Type {
	case "ai-title":
		in.Title = l.AITitle
		return
	case "last-prompt":
		in.LastPrompt = firstLine(l.LastPrompt)
		return
	}
	if l.IsSidechain {
		return
	}
	if l.GitBranch != "" {
		in.Branch = l.GitBranch
	}
	if !l.Timestamp.IsZero() {
		in.LastEventAt = l.Timestamp
	}
	var blocks []rawBlock
	var plain string
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		_ = json.Unmarshal(l.Message.Content, &plain)
	}

	if l.Type == "user" {
		you := FromYou
		if l.IsMeta {
			you = FromTool
		}
		if t.index {
			t.corpus.add(you, l.Timestamp, plain)
		}
		if !l.IsMeta && isPrompt(plain, blocks) {
			t.turn, in.TurnFiles, in.Failed = nil, 0, ""
		}
		for _, b := range blocks {
			switch b.Type {
			case "tool_result":
				t.close(b.ToolUseID)
				if t.index {
					t.corpus.add(FromTool, l.Timestamp, textOf(b.Content))
				}
			case "text":
				if t.index {
					t.corpus.add(you, l.Timestamp, b.Text)
				}
			}
		}
		if len(t.open) == 0 {
			in.Tool = ""
		}
		return
	}

	// assistant. An API error is a synthetic message: keep the real model, mark the turn failed.
	if l.Message.Model != "" && l.Message.Model != "<synthetic>" {
		in.Model = l.Message.Model
	}
	if l.IsAPIError {
		in.Failed = cmp.Or(l.Error, "error")
	} else if len(blocks) > 0 {
		in.Failed = ""
	}
	if u := l.Message.Usage; u != nil {
		if n := u.Input + u.CacheRead + u.CacheCreation; n > 0 {
			in.Tokens = n
		}
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if s := gist(b.Text); s != "" {
				in.LastText = s
			}
			if t.index {
				t.corpus.add(FromClaude, l.Timestamp, b.Text)
			}
		case "tool_use":
			if t.index {
				t.corpus.add(FromTool, l.Timestamp, string(b.Input))
			}
			t.open = append(t.open, newCall(b.ID, b.Name, b.Input))
			in.Pending = t.open[len(t.open)-1]
			in.Tool = b.Name
			in.Action = summarizeTool(b.Name, b.Input)
			if p := editedPath(b.Name, b.Input); p != "" {
				in.Edits = append(in.Edits, Edit{Path: p, At: l.Timestamp})
				if t.turn == nil {
					t.turn = map[string]bool{}
				}
				t.turn[p] = true
				in.TurnFiles = len(t.turn)
			}
		}
	}
}

// isPrompt reports whether a user line is something you typed, which starts a turn:
// text, not tool results, and not a slash command's bookkeeping.
func isPrompt(plain string, blocks []rawBlock) bool {
	text := plain
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return false
		case "text":
			text += b.Text
		}
	}
	text = strings.TrimSpace(text)
	for _, p := range []string{"<command-", "<local-command-", "[Request interrupted"} {
		if strings.HasPrefix(text, p) {
			return false
		}
	}
	return text != ""
}

var (
	reMDLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reMDMarker = regexp.MustCompile(`^(?:[-*+>]\s+|\d+[.)]\s+)+`)
	reMDSkip   = regexp.MustCompile("^(?:#|```|~~~|---|\\*\\*\\*|___|\\|)")
)

// gist is the first line of a reply worth showing on one line: headings, rules, code
// fences and tables are skipped, and inline markdown is stripped. A reply that is only
// headings falls back to the first heading's text.
func gist(text string) string {
	heading, fenced := "", false
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "```") || strings.HasPrefix(l, "~~~") {
			fenced = !fenced
			continue
		}
		if l == "" || fenced {
			continue
		}
		if reMDSkip.MatchString(l) {
			if heading == "" && strings.HasPrefix(l, "#") {
				heading = stripMarkdown(strings.TrimLeft(l, "# "))
			}
			continue
		}
		if s := stripMarkdown(reMDMarker.ReplaceAllString(l, "")); s != "" {
			return s
		}
	}
	return heading
}

func stripMarkdown(s string) string {
	s = reMDLink.ReplaceAllString(s, "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	return strings.TrimSpace(s)
}

// abbreviations that end in a period without ending the sentence
var notSentenceEnd = map[string]bool{"e.g": true, "i.e": true, "etc": true, "vs": true, "approx": true, "cf": true, "Mr": true, "Ms": true, "Dr": true, "St": true}

// FirstSentence cuts s after its first sentence, so a paragraph-long first line still
// fits a row: "Fixed for next time — 237 tests. From now on…" → "Fixed for next time — 237 tests."
func FirstSentence(s string) string {
	for i := 0; i+2 < len(s); i++ {
		if (s[i] != '.' && s[i] != '!' && s[i] != '?') || s[i+1] != ' ' {
			continue
		}
		next := rune(s[i+2])
		if !unicode.IsUpper(next) && !unicode.IsDigit(next) && next != '"' && next != '(' {
			continue
		}
		word := s[strings.LastIndexAny(s[:i], " (")+1 : i]
		if s[i] == '.' && notSentenceEnd[word] {
			continue
		}
		return s[:i+1]
	}
	return s
}

func (t *transcriptTail) close(id string) {
	for i, c := range t.open {
		if c.ID == id {
			t.open = append(t.open[:i:i], t.open[i+1:]...)
			break
		}
	}
	t.info.Pending = nil
	if n := len(t.open); n > 0 {
		t.info.Pending = t.open[n-1]
	}
}

func (t *transcriptTail) pruneEdits(now time.Time) {
	e := t.info.Edits[:0]
	for _, x := range t.info.Edits {
		if now.Sub(x.At) <= editWindow {
			e = append(e, x)
		}
	}
	t.info.Edits = e
}

type toolInput struct {
	FilePath     string `json:"file_path"`
	NotebookPath string `json:"notebook_path"`
	Command      string `json:"command"`
	Description  string `json:"description"`
	Pattern      string `json:"pattern"`
	Query        string `json:"query"`
	URL          string `json:"url"`
	Skill        string `json:"skill"`
	Questions    []struct {
		Question string `json:"question"`
	} `json:"questions"`
}

// matchLen is how much of a Bash command must appear in its permission dialog. Long
// commands wrap there, so only a prefix is compared, and whitespace is ignored.
const matchLen = 40

func newCall(id, tool string, raw json.RawMessage) *Call {
	var in toolInput
	_ = json.Unmarshal(raw, &in)
	c := &Call{ID: id, Tool: tool}
	switch {
	case tool == "AskUserQuestion" && len(in.Questions) > 0:
		c.Summary = firstLine(in.Questions[0].Question)
	case tool == "Bash" && in.Command != "":
		c.Summary = firstLine(in.Command)
		r := []rune(c.Summary)
		c.match = string(r[:min(len(r), matchLen)])
	case editedPath(tool, raw) != "":
		p := editedPath(tool, raw)
		c.Summary, c.match = p, filepath.Base(p)
	default:
		c.Summary = summarizeTool(tool, raw)
		if i := strings.Index(c.Summary, " · "); i >= 0 {
			c.Summary = c.Summary[i+len(" · "):]
		}
	}
	return c
}

func editedPath(tool string, raw json.RawMessage) string {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
	default:
		return ""
	}
	var in toolInput
	_ = json.Unmarshal(raw, &in)
	if in.FilePath != "" {
		return in.FilePath
	}
	return in.NotebookPath
}

func summarizeTool(tool string, raw json.RawMessage) string {
	var in toolInput
	_ = json.Unmarshal(raw, &in)
	arg := ""
	switch {
	case in.FilePath != "":
		arg = filepath.Base(in.FilePath)
	case in.NotebookPath != "":
		arg = filepath.Base(in.NotebookPath)
	case in.Description != "":
		arg = in.Description
	case in.Command != "":
		arg = in.Command
	case in.Pattern != "":
		arg = in.Pattern
	case in.Query != "":
		arg = in.Query
	case in.URL != "":
		arg = in.URL
	case in.Skill != "":
		arg = in.Skill
	}
	arg = firstLine(arg)
	if arg == "" {
		return tool
	}
	return tool + " · " + arg
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
