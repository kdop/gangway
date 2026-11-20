# Gangway architecture

How the parts are built to meet `requirements.md`. The design in words is in `blueprint.md`.

This file is written one part at a time. Done so far: the API, and the connection between the controller and the cli-tool. Still to write: the pieces inside each program, and the integration tests for each part.

## The API

The API is what the AI calls on the controller. It is the only way the AI reaches a host.

### Basics

- Calls and answers are JSON over HTTP. Off localhost it is HTTPS only (`KEY-06`).
- Every path starts with `/v1`.
- The session key goes in a header: `Authorization: Bearer gw_…`. It is never part of the address, so it does not end up in logs of addresses.
- The caller token goes in a second header: `Gangway-Caller: …`. Every call but the first must carry it (`KEY-05`).
- There are five calls. No call can approve a request, extend a session or change a setting (`GATE-07`, `LEN-09`).

### The five calls

| Call | What it does |
|---|---|
| `POST /v1/hello` | The first call with a new key. It makes the caller the owner of the key and gives back the caller token |
| `GET /v1/session` | Tells how the session is doing: the time left, the limits, the requests that wait or run |
| `POST /v1/requests` | Asks to run a command or read a file. The call stays open until there is something to tell |
| `POST /v1/requests/{id}/wait` | Answers "wait more" for a command that is still running. Stays open like the call above |
| `POST /v1/requests/{id}/stop` | Answers "stop" for a command that is still running |

### `POST /v1/hello`

Sends nothing but the key. Gives back the caller token and the state of the session:

```json
{
  "caller_token": "…",
  "host": "web-3",
  "seconds_left": 1740,
  "time_up": false,
  "limits": { "command_seconds": 60, "output_bytes": 102400, "answer_seconds": 60 },
  "requests": { "waiting": 0, "max_waiting": 20, "running": null }
}
```

- The first `hello` with a key wins. Any later `hello` with that key is refused (`KEY-05`).
- The limits are told up front, so the AI can plan for them.
- Everything but the caller token is the same body that `GET /v1/session` gives.

### `GET /v1/session`

Tells the AI how its session is doing. It only reads. It changes nothing.

No session ID is passed. The key in the header names the session: one key belongs to exactly one session.

```json
{
  "host": "web-3",
  "seconds_left": 1210,
  "time_up": false,
  "limits": { "command_seconds": 60, "output_bytes": 102400, "answer_seconds": 60 },
  "requests": { "waiting": 1, "max_waiting": 20, "running": "r_81c2" }
}
```

- `seconds_left` is the time until the session's time is up. It does not count extensions the user may give later.
- `time_up` is true once the time is up. New requests are refused from then on (`LEN-06`).
- `requests` tells how many requests wait in line, how many may, and the `id` of the command that runs now, if one does. An AI that lost track of a running command finds its `id` here.
- `hello` gives back this same body, plus the caller token.

### `POST /v1/requests`

Sends one of:

```json
{ "kind": "command", "command": "grep ERROR app.log | tail -n 20" }
```

```json
{ "kind": "read", "path": "/var/log/app.log" }
```

The call stays open while the request waits in line, while the prompt waits, and while the command runs. It ends with one of three answers.

**Refused.** Nothing ran.

```json
{ "state": "refused", "reason": "user" }
```

| Reason | Meaning | Requirement |
|---|---|---|
| `user` | The user pressed `n` or Ctrl+C at the prompt | `GATE-02`, `LIM-09` |
| `control_character` | The request holds a control character. No prompt was shown | `GATE-10` |
| `line_full` | Too many requests already wait. No prompt was shown | `GATE-13` |
| `protected_file` | The path is the auth key file or the host token file. No prompt was shown | `KEY-08` |
| `time_up` | The session's time is up | `LEN-06`, `LEN-07` |
| `host_away` | The connection to the host is lost for the moment. Ask again shortly | `SES-10` |

**Ended.** The command ran and is over, or the file was read.

```json
{
  "state": "ended",
  "how": "exit",
  "exit_code": 0,
  "output": "…",
  "cut": false,
  "bytes_replaced": false
}
```

| `how` | Meaning | Requirement |
|---|---|---|
| `exit` | The command ended by itself. `exit_code` is set | `GATE-01` |
| `killed_by_user` | The user pressed Ctrl+C while it ran | `LIM-08` |
| `killed_by_ai` | The AI answered "stop" | `LIM-02` |
| `killed_no_answer` | The AI gave no answer to "wait or stop" in time | `LIM-03` |
| `killed_time_up` | The session's time was up and the command reached its time limit | `LEN-08` |
| `read` | A file read. `output` is the file | `GATE-09` |

- `output` holds what the command wrote, normal and error output together, in the order it came.
- When `cut` is true, the output passed the size limit. `output` then holds the first part and ends with a note that says so (`LIM-05`).
- `output` is always text. A byte that is not text is replaced by a mark, and `bytes_replaced` is then true (`SHOW-03`). Gangway is not for moving data. An AI that needs raw bytes asks for a command that prints them as text, such as `base64`.

**Running.** The command has run for the time limit and is not over.

```json
{
  "state": "running",
  "id": "r_81c2",
  "output": "…",
  "answer_within_seconds": 60
}
```

- `output` holds what the command has written so far.
- The AI must now call `wait` or `stop` with that `id`. With neither call in time, the command is killed (`LIM-03`).

### `POST /v1/requests/{id}/wait` and `/stop`

Both send nothing. Both give back the same three kinds of answer as above.

- `wait` stays open until the command ends or the time limit passes again. `output` holds only what is new since the last answer.
- `stop` kills the command and gives back `ended` with `killed_by_ai` and the last output.
- If the command ended by itself before the call came, both give back `ended` with `exit` (`LIM-04`).

### Hanging up

The controller treats a call that is closed from the AI's side as "the AI is gone".

| The AI hangs up while | What happens | Requirement |
|---|---|---|
| The request waits in line or at the prompt | The request is removed. Nothing runs | `GATE-12` |
| The command runs | The command is killed | `LIM-07` |

After a `running` answer no call is open, so there is nothing to hang up. The answer time takes over.

One call stays open through the prompt and the command, so it can last minutes. An AI tool that cuts its calls early would kill commands by accident: Claude Code cuts a call after two minutes unless told otherwise. So the skill tells the AI to give each call a time limit of ten minutes, which Claude Code allows.

### When a call is not let in

These are not answers to a request. The call itself is turned away.

| HTTP status | Meaning | Requirement |
|---|---|---|
| 401 | No key, a wrong key, a key whose session has ended, or a missing or wrong caller token | `KEY-02`, `KEY-03`, `KEY-05`, `SES-01` |
| 404 | The `id` in `wait` or `stop` is not a running command of this session | |
| 429 | Too many wrong keys came from this address. It is blocked for a while | `KEY-07` |

A 401 says nothing about which of its causes it was. A guesser learns nothing from it.

## The connection between the controller and the cli-tool

The cli-tool opens one connection to the controller at start and keeps it open for the whole session. Every request, answer and piece of output travels on it.

### Basics

- It is a websocket: one connection that stays open and lets both sides send at any moment. The cli-tool dials. The host listens on nothing (`HOST-03`).
- It runs over Tailscale only (`CTL-10`).
- At the start the cli-tool shows its host token, so the controller knows the user (`PAIR-01`, `PAIR-09`).
- Each message is one JSON object with a `type`.
- One connection carries one session. When the connection is lost, the cli-tool tries to connect again for `RECONNECT_WINDOW`. If it does, the same session carries on. If it does not, the session is over on both sides (`SES-04`, `SES-09`).
- Each side sends a small "still here" message every few seconds. A side that hears nothing for `LINK_TIMEOUT` treats the connection as lost. A short network hiccup is ridden out and never gets that far.

### What the cli-tool trusts

The controller can be taken over, so the cli-tool trusts it with very little. A message from the controller can only do one of these:

- hand over a request, which then goes to the prompt,
- take back a request, or stop a command,
- end the session.

Each of them asks or narrows. No message type exists that approves a request, extends a session or changes a setting (`GATE-07`, `LEN-09`). A message of an unknown type is ignored and written to the log.

### Who checks what

| Check | Done by | Why there |
|---|---|---|
| The session key, the caller token, the block on wrong keys | The controller | They guard the API, before anything reaches a host |
| Control characters, the protected files, the full line | The cli-tool | They guard the host, and the host does not trust the controller to have checked |
| The `y`/`n` | The cli-tool | The gate lives on the host |
| The command time limit, the output size limit, the answer time | The cli-tool | It holds the running command, and it must act even when the controller is gone |
| The session's time and its extension | The cli-tool | Only the person at the terminal may extend |

### Starting a session

| From | Message | Holds | Meaning |
|---|---|---|---|
| cli-tool | `session_start` | A hash of the session key, the host's name, the session's length, the limits | "I am live. An AI that shows a key with this hash may ask" |
| controller | `session_ok` | Nothing | "Taken. I pass requests from now on" |

- The cli-tool makes the key and sends only a hash of it. The controller never holds the key itself. It hashes the key an AI shows and compares (`KEY-01`, `SES-06`).
- With a wrong or missing host token the controller closes the connection and no session exists (`PAIR-01`).

### A request and its life

| From | Message | Holds | Meaning |
|---|---|---|---|
| controller | `request` | `id`, `kind`, and the command or the path | "An AI asks for this" |
| cli-tool | `refused` | `id`, `reason` | "It will not run", with one of the reasons of the API |
| cli-tool | `started` | `id` | "The user pressed `y`. It runs now" |
| cli-tool | `output` | `id`, a piece of text | "It wrote this" |
| cli-tool | `still_running` | `id` | "The time limit passed. I wait for `wait` or `stop`" |
| controller | `wait` | `id` | "The AI answers: wait more" |
| controller | `stop` | `id` | "The AI answers: stop" |
| controller | `cancel` | `id` | "The AI hung up" |
| cli-tool | `ended` | `id`, `how`, `exit_code`, `cut`, `bytes_replaced` | "It is over, and this is how" |

- Every request ends with exactly one `refused` or one `ended`. Nothing is left hanging.
- On `cancel`, a request that waits is removed (`GATE-12`), and a command that runs is killed (`LIM-07`).
- After `still_running`, the cli-tool itself counts the answer time. With no `wait` and no `stop` in time, it kills the command (`LIM-03`). It does not wait for the controller to say so.
- Output is sent in pieces as it comes, not in one block at the end. The dashboard can then show it live (`CTL-07`), and a `running` answer can carry the output so far.
- The cli-tool cleans nothing in what it sends. Cleaning is only for its own terminal (`SHOW-03`).

### The session's time

| From | Message | Holds | Meaning |
|---|---|---|---|
| cli-tool | `time_up` | Nothing | "The time is up. I refuse new requests" |
| cli-tool | `extended` | The seconds left | "The user extended the session" |

These two only inform. The controller uses them for `GET /v1/session` and for the dashboard. It cannot answer them.

### Ending a session

| From | Message | Holds | Meaning |
|---|---|---|---|
| cli-tool | `session_end` | `reason` | "I am ending": the user pressed Ctrl+C, or the time ran out |
| controller | `session_end` | `reason` | "End now": the end button in the dashboard, or the host was removed |

After either one, the connection is closed and the session key is dead (`KEY-03`, `CTL-08`, `PAIR-11`).

### Pairing

Pairing happens before a host has a token, so it cannot use this connection. It has a short exchange of its own, which is written out with step 13.

### When the connection is lost

| From | Message | Holds | Meaning |
|---|---|---|---|
| cli-tool | `session_back` | The hash of the session key | "I am the cli-tool of that session. I am back" |
| controller | `session_ok` | Nothing | "Taken back. The session carries on" |

- The cli-tool sends `session_back` on a new connection, with its host token as at the start. The controller takes it back only when the token and the hash both match the session (`SES-11`).
- While the connection is away, the controller refuses new requests with the reason `host_away` (`SES-10`). Calls that are already open keep waiting.
- On the host nothing stops. A prompt that waits stays, and a command that runs keeps running. The cli-tool holds the messages it could not send and sends them after `session_ok`.
- The limits keep counting while the connection is away. A command that reaches its time limit gets no answer, so it is killed (`LIM-03`).
- Nothing is read from disk to carry on. The cli-tool is the same running program, with the key still in its memory. A cli-tool that was closed cannot come back (`SES-03`, `SES-06`).
- When `RECONNECT_WINDOW` passes with no `session_back`, the controller drops the session and the cli-tool ends it (`SES-09`).
