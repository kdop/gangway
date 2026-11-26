# Gangway releases

Each version and what it adds. A requirement in `requirements.md` names the version that delivers it. The design is in `blueprint.md`.

Rule for every version: the gate exists before the first command runs. No version ships a way to run something without a yes.

## Versions

| Version | State | What it adds |
|---|---|---|
| 0.1 | Built | The first version: one host, one user, each command approved by hand |

A state is one of three. "Planned": its requirements are written and not all are built. "Built": every requirement of it has its tests, and it is on `main`. "Released": it has a tag and its files are on the releases page.

## 0.1

- **One command, end to end.** The AI asks the controller, the controller asks the cli-tool, the cli-tool asks `y` or `n` and runs the command on yes.
- **The session key.** The cli-tool makes the key and shows the paste line. The controller refuses a call without it. The key is tied to the first AI that uses it.
- **The session length.** 30 minutes to start, another length at start, a question to extend when the time is up, and a maximum that root sets.
- **Tailscale inside both programs.** `login` takes the auth key once. A host with no Tailscale installed reaches the controller by its name. The one-machine mode stays, behind a start option.
- **The time limit of a command.** Past it, the AI is asked to wait or stop, and no answer kills the command. One Ctrl+C at the terminal kills the running command and keeps the session.
- **The log.** Every request and its answer goes to a file on the host.
- **A safe start.** The cli-tool refuses to run as root or with no terminal.
- **The skill.** The AI takes the paste line and calls the controller.
- **The install.** One command puts the cli-tool in the home folder.

Two requirements are in it in part:

- `SES-05`: there is no reconnect yet, so a lost connection ends the session at once and stops the running command.
- `LIM-06`: `COMMAND_TIME_LIMIT`, `QUESTION_WAIT` and `KILL_WAIT` only. `OUTPUT_SIZE_LIMIT` comes with the size cut.

## No version yet

Their requirements are written, and say "No version yet". The order is not decided.

| What | Done when | Requirements |
|---|---|---|
| **Reconnect.** A lost connection does not end the session at once: the cli-tool connects again within a short time. A quiet session stays live | A session lives through a short network drop with the same key, and ends when the drop is too long | `SES-04`, `SES-09` to `SES-12` |
| **Size cut.** Past the size limit, the output is cut with a note and the command runs on | A huge output comes back cut, with a note at the end, and the command still runs to its end | `LIM-05` |
| **Dashboard.** The controller shows live sessions and the log, and can end a session | The page lists a running session and its requests as they happen. The end button kills the session | `CTL-07` to `CTL-09` |
| **Login.** The dashboard asks for a social login. Only accounts on the list in the controller's settings may sign in | An account on the list gets in. Any other account is refused. The dashboard shows nothing without a login | `KEY-06`, `KEY-07`, `CTL-03` to `CTL-05` |
| **Pair a host to a user.** `login` on the host shows a one-time code. The signed-in user types it in the dashboard. The cli-tool shows the account and asks `y`/`n` | After pairing, the host's sessions show under that user only. A used or old code is refused. `n` on the host leaves the host unpaired | `PAIR-01` to `PAIR-11`, `CTL-06` |

## No requirements yet

Always allow: for the exact command, for a program, or for a list of safe read-only commands; for one session or saved.

A fresh Tailscale auth key per session, folder grants (the cli-tool names the folders the AI may read), sudo, approving from the dashboard or another device, dashboard history in SQLite, a second user, showing file changes as a diff.

## Dropped

- **Read a file**, as a second kind of request: `cat` through the gate reads a file.

## Tests

Plain Go tests, no test framework. Each acceptance test of `requirements.md` is a Go test in `acceptance/`.
