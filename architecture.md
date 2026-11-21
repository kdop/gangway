# Gangway architecture

How the parts are built to meet `requirements.md`. The design in words is in `blueprint.md`.

It has three parts: the API, the connection between the controller and the cli-tool, and the pieces inside each program. Each part ends with its integration tests.

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
| `control_character` | The request holds a control character, an invisible character, or one that changes the reading order. No prompt was shown | `GATE-10`, `GATE-16` |
| `line_full` | Too many requests already wait. No prompt was shown | `GATE-13` |
| `unknown_kind` | The cli-tool does not know this kind of request. No prompt was shown | `GATE-01` |
| `protected_file` | The path is the auth key file or the host token file. No prompt was shown | `KEY-08` |
| `time_up` | The session's time is up | `LEN-06`, `LEN-07` |
| `host_away` | The connection to the host is lost for the moment. Ask again shortly | `SES-10` |
| `session_ended` | The session ended while the request was open | `KEY-03` |

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
| `start_failed` | The command could not be started. `output` says why. Nothing ran | `GATE-11` |

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

### Integration tests for the API

These run the real controller. The test plays both other sides: it calls the API as the AI, and it sits on the connection as the cli-tool, sending and reading messages by hand. "The host" below means that hand-played cli-tool.

| Test | Start | Action | Expect |
|---|---|---|---|
| API-01 | The host has started a session | `hello` with the key | 200, with a caller token and the session's state |
| API-02 | State after API-01 | A second `hello` with the same key | 401 |
| API-03 | State after API-01 | Any call with the key and no caller token, or a wrong one | 401 |
| API-04 | State after API-01 | `GET /v1/session` | The host's name, the limits and the length are the ones the host gave in `session_start` |
| API-05 | State after API-01 | The host sends `time_up`, then `extended`. `GET /v1/session` after each | First `time_up` is true. Then it is false and `seconds_left` is the new value |
| API-06 | State after API-01 | `POST /v1/requests` with a command | The host gets one `request` with that command, letter for letter, and an `id` |
| API-07 | A request is open | The host answers `refused`, once with each reason | The call ends with `refused` and that same reason |
| API-08 | A request is open | The host sends `started`, two pieces of `output`, then `ended` with exit code 0 | The call ends with `ended`, `exit`, code 0. `output` is the two pieces joined in order |
| API-09 | A request is open | The host sends `ended` with `cut` and `bytes_replaced` both true | Both are true in the answer |
| API-10 | A request is open | The host sends `started`, one `output`, then `still_running` | The call ends with `running`, the request's `id`, the output so far and the answer time |
| API-11 | State after API-10 | `wait` with that `id`. The host then sends one more `output` and `ended` | The host gets `wait`. The call ends with `ended` and only the new output |
| API-12 | State after API-10 | `stop` with that `id`. The host sends `ended` with `killed_by_ai` | The host gets `stop`. The call ends with `ended`, `killed_by_ai` |
| API-13 | State after API-10 | The host sends `ended` by itself. Then `wait` arrives | The call ends at once with `ended`, `exit` |
| API-14 | State after API-01 | `wait` and `stop` with a made-up `id`, and with the `id` of another session's command | 404 each time. No host gets a message |
| API-15 | A request is open, not yet started | The AI closes the call | The host gets `cancel` with that `id` |
| API-16 | A request is open and started | The AI closes the call | The host gets `cancel` with that `id` |
| API-17 | State after API-10, no call open | Wait a while | The host gets no `cancel` |
| API-18 | State after API-01 | Calls with no key, with a wrong key, and with the key of an ended session | 401 each time, with the same body. No host gets a message |
| API-19 | State after API-01 | One more wrong key than `WRONG_KEY_LIMIT` from one address, then the right key | 429 for the right key. After `WRONG_KEY_BLOCK` it is let in again |
| API-20 | State after API-01 | `POST /v1/requests` with a file read. The host sends `ended` with `read` | The host gets a `request` of kind `read`. The call ends with `ended`, `read`, and the file as `output` |
| API-21 | Two requests are open at once | The host answers the second one first | Each call gets its own answer |
| API-22 | State after API-01 | The host's connection is cut. `POST /v1/requests`. Then the host sends `session_back` with the right hash, and the call is made again | First `refused` with `host_away`. Then the request reaches the host |
| API-23 | A request is open | Calls to made-up paths such as `/v1/requests/{id}/approve` and `/v1/session/extend` | 404. The host gets no message |
| API-24 | A request is open | The host sends `session_end` | The call ends with `refused` and the reason `session_ended`. The key is refused from then on |

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
- On `cancel`, a request that waits is removed (`GATE-12`), and a command that runs is killed (`LIM-07`). The cli-tool closes the request with `refused` and the reason `ai_gone`, or with `ended` and `killed_ai_gone`. A command killed because the session ended closes with `killed_session_ended`. No AI ever sees these three: they are for the dashboard and the log.
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

### Integration tests for the connection

These run the real cli-tool, with its keyboard, screen and clock in the test's hands. The test plays the controller: it takes the connection and sends and reads messages by hand.

| Test | Start | Action | Expect |
|---|---|---|---|
| CON-01 | The cli-tool is started | Read what it sends first | It shows its host token and sends `session_start` with a hash, the host's name, the length and the limits. The hash is the hash of the key in the paste line |
| CON-02 | A whole session, with requests | Search everything the cli-tool sent | The session key itself is nowhere in it |
| CON-03 | The cli-tool is started | The controller closes the connection and sends no `session_ok` | The cli-tool says it was refused and ends. No paste line is printed |
| CON-04 | Live session | Send a `request`. The test presses `y` | In this order: `started`, the `output` pieces, one `ended` with `exit`. Nothing after it for that `id` |
| CON-05 | Live session | Send a `request`. The test presses `n` | One `refused` with `user`. No `started` |
| CON-06 | Live session | Send a `request` that holds a carriage return | One `refused` with `control_character`. The screen shows no prompt |
| CON-07 | Live session | Send one more `request` than `MAX_WAITING_REQUESTS`, with no key pressed | The last one gets `refused` with `line_full` at once. The others still wait |
| CON-08 | Live session | Send a `request` to read the auth key file | One `refused` with `protected_file`. The screen shows no prompt |
| CON-09 | An approved command that runs long | Wait `COMMAND_TIME_LIMIT`. Send `wait`. Wait again. Send `stop` | `still_running`, then `still_running` again, then `ended` with `killed_by_ai` |
| CON-10 | State after the first `still_running` | Send nothing for `QUESTION_WAIT` | `ended` with `killed_no_answer`, without being asked |
| CON-11 | A prompt waits | Send `cancel` for it | The prompt is gone from the screen. One `refused` with `ai_gone`. A `y` now does nothing |
| CON-12 | A command runs | Send `cancel` for it | The command is killed. One `ended` with `killed_ai_gone` |
| CON-13 | A prompt waits | Send a message of a made-up type, `approve`, for that `id` | The prompt still waits. Nothing ran. The log holds the odd message |
| CON-14 | Live session | Send a `request` with an extra field, `"approved": true` | A prompt is shown all the same |
| CON-15 | Live session | Send a message of a made-up type, `extend` | The session's end time has not moved |
| CON-16 | Live session | Send `wait`, `stop` and `cancel` with an `id` that does not exist | Nothing changes. The log holds them |
| CON-17 | Live session | Let the session's time run out. Send a `request`. The test presses `y` at the question to extend | `time_up`. Then `refused` with `time_up` for the request. Then `extended` with the seconds left |
| CON-18 | A command runs | Send `session_end` | The command is stopped. The cli-tool says why and ends |
| CON-19 | Live session, nothing in front | The test presses Ctrl+C | The cli-tool sends `session_end` and closes the connection |
| CON-20 | A command runs | Cut the connection. Let the command end. Take the new connection | The cli-tool dials again and sends `session_back` with the same hash. After `session_ok` it sends the held `output` and `ended`, in order |
| CON-21 | Live session | Cut the connection and take no new one for `RECONNECT_WINDOW` | The cli-tool ends the session. It sends no `session_back` after that |
| CON-22 | Live session | Keep the connection open but send nothing, not even "still here", for `LINK_TIMEOUT` | The cli-tool treats the connection as lost and dials again |
| CON-23 | An approved command prints a colour code | Compare the `output` message with the screen | The message holds the code as it was. The screen does not |
| CON-24 | An approved command prints far more than `OUTPUT_SIZE_LIMIT`, then leaves a mark | Add up the `output` pieces | Their total is the limit plus the note. `ended` has `cut` true. The mark is there |

## The pieces

Each program is made of a few pieces. A piece has one job, and the table names the requirements it carries. The names are the names the folders in the code will have.

### The cli-tool

| Piece | Job | Requirements |
|---|---|---|
| `start` | Checks that it is not root and has a terminal. Picks between `login` and a session | `HOST-01`, `HOST-02`, `HOST-04` |
| `settings` | Reads the three layers and lets each only narrow the one above. Says so when it cuts a value | `SET-01` to `SET-04`, `LIM-06` |
| `secrets` | Keeps the auth key file and the host token file, readable by the owner only. Knows which paths are never read out | `HOST-05`, `PAIR-06`, `KEY-08` |
| `network` | Joins Tailscale from inside the program, and leaves it at the end | `HOST-03`, `HOST-06`, `SES-08` |
| `connection` | Holds the connection to the controller. Turns answers into messages and messages into events. Notices when it is lost, connects again, and holds what could not be sent | `GATE-07`, `SES-04`, `SES-09`, `SES-10`, `SES-11` |
| `session` | Makes the key and the paste line. Runs the session's clock: time up, the question to extend, the maximum | `KEY-01`, `KEY-04`, `SES-02`, `SES-03`, `SES-12`, `LEN-01` to `LEN-09` |
| `line` | Holds the requests that wait, in order, up to the limit | `GATE-06`, `GATE-13` |
| `checks` | Refuses a request before any prompt: control characters, invisible characters, protected files | `GATE-10`, `GATE-16`, `KEY-08` |
| `gate` | Shows one request and takes the `y` or `n`. Nothing else can say yes | `GATE-01` to `GATE-05`, `GATE-07`, `GATE-12`, `GATE-14`, `GATE-15`, `LIM-09` |
| `runner` | Runs an approved command through the shell as the plain user, or reads a file. Counts the time, cuts the output, kills when told | `GATE-08`, `GATE-09`, `GATE-11`, `GATE-15`, `GATE-17`, `GATE-18`, `LIM-01` to `LIM-05`, `LIM-07`, `LIM-08`, `LIM-11` to `LIM-13`, `SES-05`, `SHOW-03` |
| `terminal` | The only piece that writes to the screen and reads keys. Cleans what it shows. Tells one Ctrl+C from two | `SHOW-01` to `SHOW-05`, `GATE-03`, `GATE-04`, `GATE-05`, `KEY-04`, `LIM-09`, `LIM-10`, `HOST-02`, `HOST-07` |
| `loop` | Takes every event in turn and passes it to the piece it belongs to. Ends the session, and waits until no command runs | `SES-02`, `SES-05`, `LIM-08`, `LIM-09` |
| `log` | Writes each request, answer and output to the log, and starts a new file at the size limit | `LOG-01` to `LOG-05` |
| `pairing` | The exchange at `login`: the code, the `y`/`n`, the host token | `PAIR-02` to `PAIR-05`, `PAIR-10` |

How a request goes through them:

1. `connection` gets a `request` message and hands it to `checks`.
2. `checks` refuses it, or passes it to `line`.
3. `line` gives it to `gate` when nothing else is in front.
4. `gate` asks through `terminal`. On `n` it answers `refused`.
5. On `y`, `runner` runs it. `terminal` shows the output and `connection` sends it.
6. `log` writes down each of these steps.

`session` can stop this at any point: when the time is up, it tells `line` and `gate` to refuse.

### The controller

| Piece | Job | Requirements |
|---|---|---|
| `start` | Reads where to listen. Refuses a public address when no login is set up | `CTL-01`, `CTL-02` |
| `api` | The five calls. Checks the key and the caller token, blocks an address after too many wrong keys, and insists on HTTPS off localhost | `KEY-02`, `KEY-05`, `KEY-06`, `KEY-07`, `SES-01` |
| `sessions` | The live sessions, in memory only: the key's hash, the caller token, the user, the host, the state. Drops a session when it ends or does not come back in time | `SES-06`, `SES-07`, `SES-09`, `SES-11`, `KEY-03`, `PAIR-09` |
| `hostdoor` | Takes the connections from the cli-tools, over Tailscale only, and checks the host token | `CTL-10`, `PAIR-01` |
| `relay` | Ties an open API call to its request on the host and passes messages both ways. Turns a hung-up call into a `cancel` | `GATE-12`, `LIM-07`, `SES-10` |
| `hosts` | The paired hosts, kept in a small file with a hash of each token. Makes and checks pairing codes | `PAIR-02`, `PAIR-03`, `PAIR-07`, `PAIR-08`, `PAIR-10` |
| `login` | The sign-in with Google and the list of allowed accounts. Stores no password | `CTL-03`, `CTL-04`, `CTL-05` |
| `dashboard` | The pages: sessions, requests and answers as they happen, the end button, removing a host. A user sees only their own. No page can approve | `CTL-06` to `CTL-09`, `PAIR-11` |

The controller runs no command and holds no session key. It passes requests one way and answers the other.

### The skill

One folder, `skill/`, with the text that teaches the AI:

- how to read the paste line and make the `hello` call,
- the five calls and what each answer means,
- to give each call a time limit of ten minutes,
- to ask for less: plain forms such as `top -b -n 1`, and `head`, `tail` or `grep` on large output,
- to use the written form of a code, such as `\033`, never the raw character.

### What a test can replace

A test must be able to press keys, read the screen, move time forward and fake a network failure, all without a person and without waiting. So five things sit behind a narrow opening, and a test swaps in its own:

| Replaceable part | In real use | In a test |
|---|---|---|
| The keyboard | Key presses in the terminal | Keys the test sends |
| The screen | The terminal | A buffer the test reads |
| The clock | The system's time | A clock the test moves forward |
| The process starter | Starts a real command | Real commands in a scratch folder, or a fake that records what it was asked |
| The network | Tailscale and the websocket | A connection inside the test that it can cut and restore |

On the controller, two more: the clock, and the login provider, which a test replaces with a fake Google.

Nothing else is replaceable. The gate, the checks, the line, the session and the relay run as the real code in every test.

### The messages in code

The message types of the connection are written down in this file, and this file is the contract. While both programs are Go, they share one piece of code for those types, the folder `shared/`, so the two sides cannot drift apart. If the controller ever moves to another language, this file stays the contract.

What the code pins down, on top of the tables above:

| What | Value |
|---|---|
| Where the cli-tool opens its websocket on the controller | The path `/host/v1` |
| Where the controller listens when nothing is set | `127.0.0.1:8642` |
| The session key | `gw_` and then `SESSION_KEY_BITS` random bits written in hex |
| The hash of the key that the controller holds | SHA-256 of the whole key, `gw_` included, written in hex |
| The `id` of a request | Made by the controller |
| The websocket code | The library `github.com/coder/websocket`, on both sides |

### How the pieces are put together

Five patterns hold the pieces together. They are named here so the code follows them on purpose.

- **One loop owns the state.** In the cli-tool, a single loop takes every event in turn: a key press, a message from the controller, a timer that runs out, the end of a command. No two events are handled at once. "A key pressed early does not count" and "one request at a time" then hold by construction.
- **State machines.** The `gate`, the `session`, the `connection` and each request have named states, and only named events move them. Their tables are written just before each piece is built.
- **Replaceable edges.** The keyboard, the screen, the clock, the process starter and the network are handed to the code from outside. The core never reaches for the real ones itself.
- **A chain of steps.** A request passes `checks`, `line`, `gate` and `runner` in that order. Each step can refuse, and a refused request never reaches the next step.
- **A relay matched by ID.** The controller ties each open API call to its request on the host by the request's `id`, and passes messages both ways without acting on what they say.

### Integration tests for the pieces

These check that one piece hands over to the next as laid out above. They run inside one program, with the edges in the test's hands.

| Test | Start | Action | Expect |
|---|---|---|---|
| PIE-01 | cli-tool, live session | A request that `checks` refuses | It never reaches `line` or `gate`: the line is no longer, and no prompt was drawn |
| PIE-02 | cli-tool, command A runs, request B waits | A ends | `gate` gets B only now, not while A ran |
| PIE-03 | cli-tool, live session | Three requests: one refused by `checks`, one answered `n`, one answered `y` | `log` holds every step of each: the request, the answer, the output, the end |
| PIE-04 | cli-tool, an approved command prints a cursor code | Follow the output | It reaches the screen only through `terminal`, cleaned. It reaches `connection` as it was |
| PIE-05 | cli-tool, a prompt waits and two requests wait behind it | The `session` clock runs out | `gate` removes the prompt and `line` empties. All three get `refused` with `time_up` |
| PIE-06 | cli-tool, live session | A `y` key press and a `request` are put into the loop, once in each order | Key first: the prompt waits. Request first: the command runs |
| PIE-07 | cli-tool, `SESSION_MAX` below the length asked for | Start a session | `settings` hands the cut value to `session`, and `terminal` shows the message |
| PIE-08 | cli-tool, the key file kept in another folder by a setting | A request to read that file | `checks` still refuses it: it asks `secrets` for the paths, and does not carry a list of its own |
| PIE-09 | controller | A host connects and sends `session_start` | `api` accepts the key only after `sessions` holds the session, and refuses it again once the session is dropped |
| PIE-10 | controller, two hosts with live sessions | The end button for one session | `hostdoor` sends `session_end` to that host only |
| PIE-11 | controller, users A and B signed in | A's `dashboard` asks to end B's session | `sessions` refuses. Nothing is sent to any host |
| PIE-12 | cli-tool, live session, the clock in the test's hands | A `request`, then a `y` key press with no time passed. Then the clock moves by `PROMPT_MIN_SHOW` and `y` is pressed again | The first `y` goes from `terminal` to `gate` and is dropped: no `started` is sent. The second one runs the command |
| PIE-13 | cli-tool, a prompt waits for A, B waits in `line` | A `cancel` for A, then a `y` key press with no time passed | `gate` takes B from `line` and shows its prompt. The `y` is dropped. `runner` was not called |
| PIE-14 | cli-tool, `PROMPT_MIN_SHOW` set to another value in the user's settings | A `request`, then `y` just before that time, then `y` just after | `settings` hands the value to `gate`: the first `y` is dropped, the second one runs the command |
