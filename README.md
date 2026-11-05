# peekabot

A board for all your Claude Code sessions in tmux. It shows which ones are blocked on you,
which finished while you weren't looking, and what the rest are doing. Jump to any of them
in one keystroke.

![peekabot tour: the board opens on a session waiting on a permission prompt, moves through finished sessions, searches transcripts for a name, then groups by tmux window](docs/img/tour.gif)

## Why

Once you have 20+ agents, an alarm for every finished turn is just noise. peekabot is quiet.
"Needs you" is a queue sorted by how long each session has waited, and a count sits in
the tmux status line. You check it when you choose to.

| | State | Meaning |
|---|---|---|
| ⏸ | waiting | blocked on a permission prompt or a question |
| ✓ | done | finished a turn since you last looked. The row shows how: the first sentence of Claude's reply and how many files the turn wrote |
| ✗ | failed | the last turn ended in an API error (overloaded, rate limit, expired login) rather than a reply |
| ● | working | busy. An orange age means no activity for 10 minutes, so it may be stuck |
| ○ | idle | finished, and you've seen it |
| ⚠ | collision | another session in the same checkout edited the same file in the last 30 minutes, or is working on the same branch |

A waiting session shows what it wants, e.g. `wants: Bash · pnpm db:migrate`. Press `a` to
approve it or `d` to deny it without leaving the board, and the cursor moves on to the next
session that needs you. Before sending the key, peekabot checks that the prompt on that pane's
screen is the one shown on the board. If it can't tell, it sends nothing and tells you to jump.

Press `/` to search every session's transcript, not just titles. It's handy when all you
remember is a name or a phrase from your prompt:

![searching for "ada" finds three sessions, with the matching line from each prompt](docs/img/search.png)

A session counts as seen when its pane is visible, when you jump to it, or when you press `x`.
With the optional focus hook, focusing the pane anywhere in tmux also counts.

## Install

```sh
brew tap jdbrewer/peekabot https://github.com/jdbrewer/peekabot
brew trust --formula jdbrewer/peekabot/peekabot   # Homebrew 7+ asks you to trust third-party formulae
brew install peekabot
```

Or download a binary for macOS or Linux from [Releases](https://github.com/jdbrewer/peekabot/releases),
or `go install github.com/jdbrewer/peekabot@latest` (lands in `$(go env GOPATH)/bin`).

Then add the tmux bindings:

```sh
peekabot tmux-snippet              # review it, then append it to ~/.tmux.conf
tmux source ~/.tmux.conf
```

The snippet binds `prefix a` to open the board in a popup (Enter jumps and the popup closes).
It adds the badge to `status-right` and sets `status-interval 5`. It also adds an optional
`pane-focus-in` hook and sets `focus-events on`, which the hook needs.

Requires tmux 3.2+ (popups) and Claude Code. It runs on macOS and Linux. CI runs an
end-to-end check on both against a fake fleet of sessions in real tmux.

### Coming from ccfleet

peekabot was called ccfleet until v0.2.0. How you move over depends on how you installed it:

- **Homebrew:** Homebrew 7+ wants you to trust the formula under its new name first:

  ```sh
  brew update
  brew trust --formula jdbrewer/ccfleet/peekabot
  brew upgrade ccfleet
  ```

  Homebrew keeps a `ccfleet` command for now, so your tmux bindings still open the board.
  The board shows a reminder to update them.
- **A release binary you copied somewhere** (e.g. `~/.local/bin/ccfleet`): install peekabot
  any way above, then delete the old binary (`which ccfleet` shows where it is).
- **`go install`:** the old path now fails with `module declares its path as:
  github.com/jdbrewer/peekabot`. Run `go install github.com/jdbrewer/peekabot@latest`, then
  `rm "$(go env GOPATH)/bin/ccfleet"`.

Then swap `ccfleet` for `peekabot` in your tmux.conf, or paste a fresh `peekabot tmux-snippet`.
Your seen marks carry over on their own. The old `@ccfleet_seen` focus hook and `CCFLEET_*`
environment variables keep working in the meantime.

## Commands

- `peekabot`: the board. Pass `--client '#{client_tty}'` when you run it from a popup.
- `peekabot ls [--json]`: a snapshot for scripts
- `peekabot grep [--json] TERMS...`: which live sessions mention all the terms, with a snippet (your prompts first)
- `peekabot badge`: `⏸1 ✓2 ●6` with tmux colour codes
- `peekabot tmux-snippet`: config to paste into tmux.conf

## Keys

| Key | Action |
|---|---|
| `j` `k`, counts like `5j` | move |
| `gg` `G` | top, bottom |
| `C-d` `C-u` / `C-f` `C-b` | half page / full page |
| `]` `[` | next / previous session that needs you |
| `Enter` `o` | jump to its tmux pane |
| `x` | mark seen without jumping |
| `a` `d` | approve / deny the selected session's permission prompt (Yes / No) without jumping |
| `p` | toggle the live peek (beside the list at ≥150 columns, below it otherwise) |
| `l` `h` / `Tab` | focus peek / list. In the peek, `j k gg G C-d C-u` scroll |
| `/` | search by space-separated terms. Matches names, titles, branches **and full transcripts**: your prompts, Claude's replies, tool output. Rows matched by transcript show a snippet. `Enter` keeps the search, `Esc` clears it |
| `t` | group by state, tmux window, or repo |
| `?` | help |
| `q` | quit |

## How it works

peekabot only reads files. It needs no hooks and no daemon. It joins:

- `~/.claude/sessions/<pid>.json`: status, tmux pane, cwd (live pids only)
- `~/.claude/projects/*/<sessionId>.jsonl`: title, branch, current tool, context size, edited files (tailed by byte offset)
- `~/.claude/teams/*/config.json`: agent-team lead and teammates
- `tmux list-panes` / `capture-pane`: which panes are visible, plus the peek

`CLAUDE_CONFIG_DIR` and `XDG_STATE_HOME` are honoured.

## Limitations

- **It reads Claude Code's internal files.** Those formats aren't a public API and can change
  in any release. The parsing is isolated in `internal/fleet`. If the session files stop
  parsing, peekabot falls back to the documented `claude agents --json` and finds panes through
  the process tree. You keep the board, but lose how long each session has been in its state.
- **Jump and peek need tmux.** `peekabot ls` and `peekabot grep` work anywhere.
- **Search covers live sessions only**, and only the first 16KB of each tool output.
- **Approve and deny only answer prompts peekabot can match to the screen**: Bash commands
  and file edits. For MCP tools, web fetches and subagents' prompts it refuses, and you jump
  to answer. It presses only the plain Yes or No, never "always allow". Questions
  (AskUserQuestion) are never answered from the board.
- **Collisions only see Claude's Edit and Write tools.** A file changed with `sed` in Bash isn't counted.
- **"Done" depends on knowing what you've looked at**: visible panes, jumps, `x`, and the
  optional focus hook. A pane you read without focusing it still counts as unseen.
- **Context % assumes a 200k window**, unless the model name or token count says 1M.
- macOS and Linux only.

## Similar tools

| Tool | What it is | Where peekabot differs |
|---|---|---|
| Claude Code's agent view (`claude agents`) | Built-in dashboard for dispatching and supervising background sessions | Watches the interactive sessions you already run in tmux panes, and adds a live peek, jump, transcript search and collision warnings |
| [tmux-claude-hatch](https://github.com/craftzdog/tmux-claude-hatch) | tmux plugin that runs a Claude session per project in a popup, with an fzf picker (status and preview) and a bell on the window that needs you | Closest in spirit. peekabot hosts nothing: it watches sessions wherever they already run, uses a quiet queue instead of bells, and adds full-transcript search, collisions and teams |
| [claude-squad](https://github.com/smtg-ai/claude-squad) | Launches and manages agents, each in its own tmux session and git worktree | peekabot launches nothing. It's read-only over whatever you already run |
| [ccmanager](https://github.com/kbwo/ccmanager) | Session manager across git worktrees, no tmux | peekabot is tmux-native: popup, jump, pane peek |
| [atm](https://docs.rs/crate/atm) | Rust dashboard plus daemon that can spawn, kill and message agents, with cost tracking | peekabot has no daemon and doesn't control agents. It stays out of the way and tells you where to look |

The screenshots come from a made-up fleet. `python3 scripts/demo/demo.py` rebuilds them,
`--keep` leaves the demo tmux session running so you can try the board without real sessions,
and `--check` runs the same end-to-end assertions as CI.
