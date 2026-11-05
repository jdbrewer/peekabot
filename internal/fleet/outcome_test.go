package fleet

import "testing"

func TestGist(t *testing.T) {
	cases := map[string]string{
		"Fixed: the cookie path was wrong.\n\nDetails…":            "Fixed: the cookie path was wrong.",
		"## Summary\n\nAll **three** fixes are in `main`.":         "All three fixes are in main.",
		"Live at the same link: [Mockups](https://example.com/a).": "Live at the same link: Mockups.",
		"- Moved the retry into `PinView`\n- Dropped the cache":    "Moved the retry into PinView",
		"```go\nx := 1\n```\nThat compiles now.":                   "That compiles now.",
		"| a | b |\n|---|---|\n\nTable above.":                     "Table above.",
		"# Release notes":                                          "Release notes",
		"   ":                                                      "",
	}
	for in, want := range cases {
		if got := gist(in); got != want {
			t.Errorf("gist(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	cases := map[string]string{
		"Fixed for next time — 237 tests. From now on one family per day.": "Fixed for next time — 237 tests.",
		"Done! Everything is pushed.":                                      "Done!",
		"Use a flag, e.g. --fast. Then rerun.":                             "Use a flag, e.g. --fast.",
		"Bumped to v1.2. 3 tests changed.":                                 "Bumped to v1.2.",
		"Read config.json. then stop":                                      "Read config.json. then stop", // lowercase: not a new sentence
		"No period at all":                                                 "No period at all",
	}
	for in, want := range cases {
		if got := FirstSentence(in); got != want {
			t.Errorf("FirstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTurnOutcome(t *testing.T) {
	tail := newTranscriptTail("", false)
	feed := func(lines ...string) {
		for _, l := range lines {
			tail.consume([]byte(l))
		}
	}
	edit := func(id, path string) string {
		return `{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"tool_use","id":"` + id + `","name":"Edit","input":{"file_path":"` + path + `"}}]}}`
	}
	feed(`{"type":"user","message":{"content":"fix the redirect"}}`,
		edit("1", "/r/a.go"), edit("2", "/r/b.go"), edit("3", "/r/a.go"),
		`{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"## Done\nFixed the loop. It was the cookie path."}]}}`)
	s := Session{Info: tail.info}
	if s.Outcome() != "Fixed the loop. · 2 files" || s.Info.Failed != "" {
		t.Errorf("outcome = %q failed=%q", s.Outcome(), s.Info.Failed)
	}

	feed(`{"type":"user","message":{"content":"<command-name>/model</command-name>"}}`)
	if tail.info.TurnFiles != 2 {
		t.Error("a slash command reset the turn")
	}
	feed(`{"type":"user","message":{"content":"now the tests"}}`,
		`{"type":"assistant","isApiErrorMessage":true,"error":"server_error","message":{"model":"<synthetic>","content":[{"type":"text","text":"API Error: 529 Overloaded. Try again."}]}}`)
	s = Session{Info: tail.info}
	if s.Info.Failed != "server_error" || s.Info.Model != "claude-opus-5-5" || s.Outcome() != "API Error: 529 Overloaded." {
		t.Errorf("after an API error: failed=%q model=%q outcome=%q", s.Info.Failed, s.Info.Model, s.Outcome())
	}
	feed(`{"type":"user","message":{"content":"try again"}}`)
	if tail.info.Failed != "" || tail.info.TurnFiles != 0 {
		t.Errorf("a new prompt kept failed=%q files=%d", tail.info.Failed, tail.info.TurnFiles)
	}
}
