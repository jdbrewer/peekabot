# Approve from the board

Answer a session's permission prompt from peekabot without jumping to its pane:
`a` approves, `d` denies. The board shows what is being asked, so you can clear the
waiting queue from the popup.

## What a prompt looks like (probed on Claude Code 2.1.318, 2026-11-05)

- The session file gains `"waitingFor"`: `"permission prompt"` for tool permission,
  `"input needed"` for AskUserQuestion. The folder-trust dialog appears before a session
  file exists.
- The dialog sits at the bottom of the pane: a header (`Bash command`, `Create file`, …),
  the command or file, then `Do you want to proceed?` / `Do you want to create notes.txt?`,
  numbered options, and the footer `Esc to cancel · Tab to amend`.
- **Option numbers vary.** Bash had 4 (`1. Yes`, `2. Yes, and always allow access to <dir>…`,
  `3. Yes, and switch to auto mode…`, `4. No`). Write had 3 (`No` is 3). Never send a fixed digit.
- A digit selects and confirms immediately. `No` records a rejected tool_result, and Claude
  stops with "What should Claude do instead?", so the session goes idle and shows as done.
- The pending call is the last main-thread `tool_use` with no `tool_result` yet. A Bash or
  Write `tool_use` is written before its prompt appears, but **an AskUserQuestion `tool_use`
  isn't written until the question is answered**. While a question is open, the board falls
  back to `waitingFor` and shows "asks: a question".

## Guards (all must hold right before the key is sent)

1. A fresh snapshot still has the session `waiting`, and `waitingFor` is `permission prompt`,
   or missing on older versions. `input needed` never gets an answer from here.
2. Its pending tool_use id is the one the board showed when you pressed the key.
3. The visible pane (not scrollback) ends in a parsable permission dialog: a
   `Do you want to …?` line, numbered options, then `Esc to cancel` as the last line.
4. The dialog shows the pending call: the first 40 characters of the Bash command, or the
   file's basename, compared with whitespace removed. If peekabot can't confirm the match,
   it refuses and tells you to jump.
5. The key sent is the number of the option labelled exactly `Yes` or `No`.

"Always allow" (option 2) is left out on purpose: what it grants changes from prompt to prompt.

Any failed guard sends nothing and flashes why. Refusing is always the safe outcome.

## UI

- Waiting rows show the call (`Bash · pnpm db:migrate`) or the question (`asks · …`) in the doing column.
- Detail line: `wants: Bash · pnpm db:migrate   a approve · d deny`, or `asks: …   ⏎ jump to answer`.
- After an answer: flash `approved api-3f: …`, mark it seen, and move to the next session that needs you.
- `ls` shows the pending call for waiting sessions. `ls --json` adds `waitingFor` and `transcript.pending`.

No opt-in flag: nothing is sent unless you press `a` or `d` on a prompt that passed every guard.
That is the same single keystroke Claude Code itself asks for.

## Tests

- Unit: dialog parser on probed screens (Bash, Write), the trust dialog, a question dialog,
  and a dialog scrolled up by later output. Pending-call tracking. `Answer` with fake screen and send.
- e2e (`demo.py --check`): the waiting demo pane shows the real dialog layout. Its fake claude
  logs the keys it receives. `a` must send `1` and `d` must send `4`.
