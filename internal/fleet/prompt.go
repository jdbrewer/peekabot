package fleet

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Prompt is a Claude Code permission dialog read off the bottom of a pane:
//
//	Bash command
//	 touch probe-a.txt
//	Do you want to proceed?
//	❯ 1. Yes
//	  2. Yes, and always allow access to …
//	  4. No
//	Esc to cancel · Tab to amend
//
// The option numbers differ from prompt to prompt, so Yes and No are found by label.
type Prompt struct {
	Question string
	Yes, No  int    // option numbers labelled exactly "Yes" and "No"; 0 if absent
	text     string // everything from the dialog's top rule to the footer, whitespace removed
}

var (
	reQuestion = regexp.MustCompile(`^\s*Do you want to .+\?\s*$`)
	reOption   = regexp.MustCompile(`^\s*(?:❯\s*)?([1-9])\.\s+(.+?)\s*$`)
	reRule     = regexp.MustCompile(`^\s*─{10,}\s*$`)
)

// dialogSpan bounds how far up from the footer the question may be, and how far up from
// the question the dialog body may reach.
const dialogSpan = 30

// ParsePrompt finds a permission dialog that is live: its footer is the last thing on
// screen. A dialog that later output pushed up is history, not a prompt.
func ParsePrompt(screen string) (Prompt, bool) {
	lines := strings.Split(strings.TrimRight(screen, " \n"), "\n")
	foot := len(lines) - 1
	if foot < 0 || !strings.Contains(lines[foot], "Esc to cancel") {
		return Prompt{}, false
	}
	q := -1
	for i := foot - 1; i >= max(foot-dialogSpan, 0); i-- {
		if reQuestion.MatchString(lines[i]) {
			q = i
			break
		}
	}
	if q < 0 {
		return Prompt{}, false
	}
	p := Prompt{Question: strings.TrimSpace(lines[q])}
	for _, l := range lines[q+1 : foot] {
		m := reOption.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "Yes":
			p.Yes = n
		case "No":
			p.No = n
		}
	}
	if p.Yes == 0 && p.No == 0 {
		return Prompt{}, false
	}
	top := max(q-dialogSpan, 0)
	for i := q - 1; i >= top; i-- {
		if reRule.MatchString(lines[i]) {
			top = i + 1
			break
		}
	}
	p.text = squeeze(strings.Join(lines[top:foot], "\n"))
	return p, true
}

// Shows reports whether the dialog is about c. Calls with nothing reliable to compare
// never match, so the board refuses rather than guesses.
func (p Prompt) Shows(c *Call) bool {
	return c != nil && c.match != "" && strings.Contains(p.text, squeeze(c.match))
}

func squeeze(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// Answerable reports whether the board may offer to answer s's prompt. Answer still
// re-checks everything against a fresh snapshot and the screen before sending a key.
func (s Session) Answerable() bool {
	return s.State == Waiting && s.Tmux.Valid() && s.Info.Pending != nil &&
		(s.WaitingFor == "" || s.WaitingFor == WaitingPermission)
}

// WaitingPermission is the sessions file's waitingFor for a tool permission prompt.
// AskUserQuestion shows up as "input needed" and is never answered from the board.
const WaitingPermission = "permission prompt"

// Answer presses Yes (approve) or No on session id's permission prompt for call callID.
// Every check runs again right before the key is sent; any doubt sends nothing.
func (f *Fleet) Answer(id, callID string, approve bool) error {
	ss, err := f.Snapshot()
	if err != nil {
		return err
	}
	var s *Session
	for i := range ss {
		if ss[i].ID == id {
			s = &ss[i]
		}
	}
	switch {
	case s == nil:
		return errors.New("the session has exited")
	case s.State != Waiting:
		return errors.New("it is no longer waiting")
	case s.WaitingFor != "" && s.WaitingFor != WaitingPermission:
		return errors.New("it is waiting for " + s.WaitingFor + ", not a permission")
	case s.Info.Pending == nil || s.Info.Pending.ID != callID:
		return errors.New("its prompt changed; look again")
	case !s.Tmux.Valid():
		return errors.New("it is not in tmux")
	}
	p, ok := ParsePrompt(f.screen(s.Tmux.Pane))
	if !ok {
		return errors.New("no permission prompt on its screen")
	}
	if !p.Shows(s.Info.Pending) {
		return errors.New("can't confirm the prompt on screen is for " + s.Info.Pending.Tool)
	}
	n := p.Yes
	if !approve {
		n = p.No
	}
	if n == 0 {
		return errors.New("the prompt has no plain Yes/No option")
	}
	return f.send(s.Tmux.Pane, strconv.Itoa(n))
}

// captureScreen returns the visible part of a pane as plain text, without scrollback.
func captureScreen(pane string) string {
	out, _ := exec.Command("tmux", "capture-pane", "-p", "-t", pane).Output()
	return string(out)
}

func sendKey(pane, key string) error {
	return exec.Command("tmux", "send-keys", "-t", pane, "-l", key).Run()
}
