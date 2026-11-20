# Gangway plan

Small steps. Each step is delivered alone, tried by Kostas, then the next one starts. The design is in `blueprint.md`.

Rule for all steps: the gate exists before the first command runs. No step ships a way to run something without a yes.

## Before any code

Nothing blocks step 1. The language is Go, for the cli-tool and the controller. Where the controller listens is a setting; the default is localhost.

## Steps

| # | Step | Done when |
|---|---|---|
| 1 | **One command, end to end.** Controller passes one request to the cli-tool. The cli-tool asks `y`/`n` and runs it on yes. Both on one machine for now | A `curl` to the controller makes a prompt show in the cli-tool. `y` returns the output. `n` returns "refused" and nothing runs |
| 2 | **Login and join Tailscale.** `login` takes the auth key once and stores it. On start the cli-tool joins through `tsnet` and reaches the controller over Tailscale | Step 1 works from a host with no Tailscale installed. A second start asks for no key. After Ctrl+C the host leaves the network |
| 3 | **Session key.** The cli-tool makes the key and shows the paste line. The controller refuses calls without it | A call with no key or a wrong key is refused. Ctrl+C kills the key |
| 4 | **Key limits.** The session has a length: 30 minutes by default, another length on start, a question to extend when time is up, and a maximum. Tied to the first AI that uses it | A call after the time is up is refused. No answer to the extend question ends the session. The session ends at the maximum with no question. A second caller with the same key is refused |
| 5 | **Read a file.** A second kind of request, through the same gate | The AI gets the file on yes and "refused" on no |
| 6 | **Time and size limits.** Past a limit, the AI is asked to wait or stop, keep receiving or stop. No answer in 60 seconds kills the command. One Ctrl+C at the terminal kills the running command and keeps the session | `sleep 300` comes back as "still running" after the time limit. "stop" kills it, "wait" lets it run on, and no answer kills it after 60 seconds. Ctrl+C kills it at once and the session lives on. A huge output comes back cut, with the question |
| 7 | **Log.** Every request and its answer goes to a file on the host | After a session, the log shows each request, the answer and the time |
| 8 | **Dashboard.** The controller shows live sessions and the log, and can end a session | The page lists a running session and its requests as they happen. The end button kills the session |
| 9 | **Safe start.** The cli-tool refuses to run as root or in the background | Starting it with sudo prints why and exits |
| 10 | **Skill.** The AI takes the paste line and calls the controller | In a real Claude Code session, the AI runs a command on the host after one paste and one `y` |
| 11 | **Install.** One command puts the cli-tool in the home folder | A fresh host goes from nothing to a running session with the README steps only |
| 12 | **Login.** The dashboard asks for a social login. Only accounts on the list in the controller's settings may sign in. Comes before the controller is put on a public address | An account on the list gets in. Any other account is refused. The dashboard shows nothing without a login |
| 13 | **Pair a host to a user.** `login` on the host shows a one-time code. The signed-in user types it in the dashboard. The cli-tool shows the account and asks `y`/`n` | After pairing, the host's sessions show under that user only. A used or old code is refused. `n` on the host leaves the host unpaired |

## Tests

Few and lean. Plain Go table tests, no test framework. The gate and the key limits get tests first: they are the parts that must not break.

## Second stage

Always allow: for the exact command, for a program, or for a list of safe read-only commands; for one session or saved.

## Later, not planned yet

A fresh Tailscale auth key per session, folder grants (the cli-tool names the folders the AI may read), sudo, approving from the dashboard or another device, dashboard history in SQLite, a second user.
