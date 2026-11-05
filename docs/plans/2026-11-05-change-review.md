# Change review

Press `c` to swap the peek pane for what the selected session changed: the files it wrote,
their state in git, and the diff. You can check an agent's work before you jump to it.

## Scope

- **Files:** every path the session wrote with Edit, Write, MultiEdit or NotebookEdit, over
  the whole session (`Info.Written`). The 30-minute `Edits` list stays for collisions. Files
  changed through Bash aren't seen, the same limit as collisions.
- **Repo:** each file is grouped by its own `git rev-parse --show-toplevel`, so worktrees
  and sessions that edit several repos work. Files outside any repo are listed without a diff.
- **Per file, against HEAD** (the empty tree if the repo has no commits yet):
  modified `+a −d`, new (untracked or added), deleted, or no diff against HEAD.
  The last case is either committed already or reverted.
- **Commits:** `git log` since the session started, touching those files, so committed work
  isn't mistaken for "nothing changed". Other sessions' commits to the same files show too,
  and that's fine: it's the same code.
- **Diff:** `git diff --color=always HEAD -- files`, plus `--no-index` diffs for untracked files.

## UI

- `c` toggles the peek between the live pane and changes. It turns the peek on if it was off.
  The title says which one is showing.
- The changes view opens at the top, and it scrolls like the peek (`l`, then `j/k/gg/G/C-d/C-u`).
- It refreshes when the selection changes, when toggled, and on the tick at most every 3s.
  Git is cheap, but not every second on a big repo.

## Tests

- Unit: a temp repo with a modified file, a new untracked file, a committed file, a deleted
  file, and a file outside any repo. Also a repo with no commits.
- e2e: in the demo, api-3f's files exist, with a base commit and working-tree edits. `c` on
  api-3f shows `policy.ts` with its counts. A new screenshot, `docs/img/changes.png`.
