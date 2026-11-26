# Gangway plan

Small steps. Each step is delivered alone, tried by hand, then the next one starts. The design is in `blueprint.md`.

Rule for all steps: the gate exists before the first command runs. No step ships a way to run something without a yes.

## Before the first step

The language is Go, for the cli-tool and the controller. Where the controller listens is a setting; the default is localhost.

## The MVP

The first thing built. It shows the three parts working together on one machine, on localhost, with no Tailscale. It cuts across the steps below; the requirements it delivers are listed in `requirements.md`.

| In | After |
|---|---|
| Session key and the paste line | Tailscale and `login` |
| Prompt answered by one key, `y` or `n` | Session length and the extend question |
| Command runs through the shell | Key tied to the first AI, and the wrong-key block |
| Requests with control characters refused, output cleaned | Size cut |
| Refuses to start as root or without a terminal | Reconnect after a lost connection |
| Ctrl+C rules | Log file |
| Time limit, with wait or stop | Dashboard, Google login, pairing |
| Waiting line, up to `MAX_WAITING_REQUESTS` | File reads |
| API: `hello`, `requests`, `wait`, `stop` | Admin settings in `/etc/gangway/` |
| Skill | Install |

In the MVP the session ends on Ctrl+C, when the cli-tool closes, or when the connection is lost.

Done when: in a real Claude Code session, one paste and one `y` runs a command on the machine and the AI gets the output.

## Steps

| # | Step | Done when |
|---|---|---|
| 1 | **One command, end to end.** Controller passes one request to the cli-tool. The cli-tool asks `y`/`n` and runs it on yes. Both on one machine for now | A `curl` to the controller makes a prompt show in the cli-tool. `y` returns the output. `n` returns "refused" and nothing runs |
| 2 | **Login and join Tailscale.** `login` takes the auth key once and stores it, on the host and on the controller. Both join through `tsnet`. The cli-tool reaches the controller over Tailscale by its name, and the controller gives the API address for the paste line. The one-machine way stays, behind a start option | Step 1 works from a host with no Tailscale installed, with the controller on another machine. A second start asks for no key. After Ctrl+C the host leaves the network. The acceptance tests of the MVP still pass in the one-machine mode |
| 3 | **Session key.** The cli-tool makes the key and shows the paste line. The controller refuses calls without it | A call with no key or a wrong key is refused. Ctrl+C kills the key |
| 4 | **Key limits.** The session has a length: 30 minutes by default, another length on start, a question to extend when time is up, and a maximum. Tied to the first AI that uses it | A call after the time is up is refused. No answer to the extend question ends the session. The session ends at the maximum with no question. A second caller with the same key is refused |
| 5 | **Read a file.** A second kind of request, through the same gate | The AI gets the file on yes and "refused" on no |
| 6 | **Time and size limits.** Past the time limit, the AI is asked to wait or stop, and no answer in 60 seconds kills the command. Past the size limit, the output is cut with a note and the command runs on. One Ctrl+C at the terminal kills the running command and keeps the session | `sleep 300` comes back as "still running" after the time limit. "stop" kills it, "wait" lets it run on, and no answer kills it after 60 seconds. Ctrl+C kills it at once and the session lives on. A huge output comes back cut, with a note at the end, and the command still runs to its end |
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

A fresh Tailscale auth key per session, folder grants (the cli-tool names the folders the AI may read), sudo, approving from the dashboard or another device, dashboard history in SQLite, a second user, showing file changes as a diff.
