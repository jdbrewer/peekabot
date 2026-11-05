# One-line results for done sessions

A done row says how the turn ended, so you can triage the done queue with `x`
without jumping: `✓ web-f8  Fix login redirect loop  Fixed: the cookie was on the wrong path. · 2 files`.

## What a turn's end looks like (checked against local transcripts, 2026-11-05)

- Claude's final reply usually leads with the outcome, so its first sentence is the result.
  First lines are often a whole paragraph, so the row shows the first sentence, and the
  detail line keeps the whole first line (`said:`).
- Replies can open with markdown: `## Summary`, `**bold**`, backticks, `[text](url)` links.
  Skip headings, rules and code fences, and strip inline markup.
- A turn that died on an API error ends with an assistant message carrying
  `"isApiErrorMessage":true`, an `error` kind (`server_error`, `rate_limit`,
  `authentication_failed`) and model `<synthetic>`. The session goes idle like a finished one.
  Show it as **failed** (✗, red), not ✓. Don't record `<synthetic>` as the session's model.
  (That was a bug: the detail line could show `<synthetic>`.)

## Model

- `LastText`: first meaningful line of the last assistant text, with markdown stripped.
- `Failed`: the error kind when the last assistant message was an API error. A new prompt or
  a normal reply clears it.
- `TurnFiles`: distinct files Edit/Write touched since your last prompt.
- `Session.Outcome()`: first sentence of `LastText`, plus ` · N files`.

## UI

- Done rows show `Outcome()` in the doing column. Failed ones are red with a ✗ glyph.
- Detail line: `failed (server_error): API Error: 529 …`.
- `ls` DOING shows the outcome for done sessions. `ls --json` adds `failed` and `turnFiles`.

## Tests

Unit tests: the gist and first-sentence rules on the sampled shapes, turn-file counting
across prompts, error turns, and the `<synthetic>` model. In the demo, web-f8 edits 2 files,
and docs-2a ends on a 529 so the screenshots show a failed row.
