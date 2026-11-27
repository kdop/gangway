# The Gangway API by hand

How a person calls the controller's API with `curl`, with no AI. The full contract is in `architecture.md`.

The API has five calls. None of them can approve a command: that is done on the host, with a key press.

## What you need

A running session. On one machine, with no Tailscale, use two terminals:

```
gangway-controller -local
gangway --local
```

The controller listens on `127.0.0.1:8642`. The cli-tool prints a line of this form:

```
/gangway http://127.0.0.1:8642 gw_4f9c…
```

The second word is the address. The third is the session key. In a third terminal, put both in shell variables:

```
GW=http://127.0.0.1:8642
KEY=gw_4f9c…
```

You also need `curl`.

## Say hello, once

```
curl -sS -X POST "$GW/v1/hello" -H "Authorization: Bearer $KEY"
```

```json
{
  "caller_token": "gwc_81b2…",
  "host": "web-3",
  "seconds_left": 1771,
  "time_up": false,
  "limits": { "command_seconds": 60, "answer_seconds": 60 },
  "requests": { "waiting": 0, "max_waiting": 20, "running": null }
}
```

Put the caller token in a third variable:

```
CALLER=gwc_81b2…
```

- Every later call sends it in the header `Gangway-Caller`, next to the key.
- `hello` works once per key. A second `hello` gets a 401.
- The token is given once. If you lose it, start a new session.
- The other fields are told in "Look at the session" below.

## Ask for a command

```
curl -sS -X POST "$GW/v1/requests" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER" \
  -d '{"kind":"command","command":"uname -s"}'
```

- The host shows the command and asks `y` or `n`.
- The call stays open until the person at the host presses a key and the command ends. `curl` waits that long by itself.
- If you stop `curl` with Ctrl+C, Gangway takes the request back. A command that already runs is killed.
- The command runs through `/bin/sh`, so pipes, `>`, `;` and `&&` work.
- The body is JSON. Inside the command, write `"` as `\"` and `\` as `\\`.
- The command may hold line breaks and tabs. It may hold no other control character, no invisible character, and no character that changes the reading order.
- The body may be 512 KiB at most.

## The answers

Every answer to a request is JSON with a `state`, and comes with HTTP status 200. There are three states.

### `refused`: nothing ran

```json
{"state":"refused","reason":"user"}
```

| `reason` | Meaning |
|---|---|
| `user` | The person at the host pressed `n`, or Ctrl+C at the prompt |
| `control_character` | The command holds a character that is not allowed. No prompt was shown |
| `line_full` | Too many of your requests already wait: more than `max_waiting`. No prompt was shown |
| `time_up` | The session's time is up. The person at the host is asked whether to extend it |
| `session_ended` | The session ended while the request was open |

### `ended`: the command ran and is over

```json
{"state":"ended","how":"exit","exit_code":0,"output":"Linux\n","cut":false,"bytes_replaced":false}
```

| `how` | Meaning |
|---|---|
| `exit` | The command ended by itself. `exit_code` is set |
| `killed_by_user` | The person at the host pressed Ctrl+C while it ran |
| `killed_by_ai` | You called `stop` |
| `killed_no_answer` | You called neither `wait` nor `stop` in time |
| `killed_time_up` | The session's time was up when the command reached its time limit |
| `start_failed` | The command could not be started. `output` says why |

| Field | Meaning |
|---|---|
| `exit_code` | The exit code of the command. It is there only when `how` is `exit` |
| `output` | What the command wrote, normal and error output together, in order |
| `cut` | True when the output was too long. `output` then holds the first part and ends with a note |
| `bytes_replaced` | True when the output held bytes that are not text. Each one was replaced by `�` |

A command that fails still gives `exit`. Its `exit_code` is then not 0:

```json
{"state":"ended","how":"exit","exit_code":2,"output":"ls: cannot access '/nothing-here': No such file or directory\n","cut":false,"bytes_replaced":false}
```

A command that was killed has no `exit_code`:

```json
{"state":"ended","how":"killed_by_user","output":"","cut":false,"bytes_replaced":false}
```

### `running`: the command is not over

The command has run for `command_seconds` and still runs.

```json
{"state":"running","id":"r_bbd82b3022e77412","output":"one\n","answer_within_seconds":60}
```

| Field | Meaning |
|---|---|
| `id` | The name of this command, for `wait` and `stop` |
| `output` | What the command has written so far |
| `answer_within_seconds` | How long you have to call `wait` or `stop`. After that the command is killed |

## A command that runs long

After a `running` answer, call one of these two, with the `id` from that answer:

```
curl -sS -X POST "$GW/v1/requests/r_bbd82b3022e77412/wait" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER"
curl -sS -X POST "$GW/v1/requests/r_bbd82b3022e77412/stop" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER"
```

Both send no body.

- `wait` lets the command run on. The call stays open until the command ends or `command_seconds` pass again. It gives `ended` or `running`. `output` holds only what is new since the last answer.
- `stop` kills the command and gives `ended` with `killed_by_ai`.
- If the command ended before your call came, both give that `ended` answer, once.
- `stop` works only after a `running` answer. To stop a command before that, stop `curl` with Ctrl+C.
- A call with an `id` that does not wait for `wait` or `stop` gets a 404.

## Look at the session

```
curl -sS "$GW/v1/session" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER"
```

```json
{
  "host": "web-3",
  "seconds_left": 1679,
  "time_up": false,
  "limits": { "command_seconds": 60, "answer_seconds": 60 },
  "requests": { "waiting": 0, "max_waiting": 20, "running": "r_bbd82b3022e77412" }
}
```

| Field | Meaning |
|---|---|
| `host` | The name of the host |
| `seconds_left` | The time until the session's time is up. Only the person at the host can extend it |
| `time_up` | True once the time is up. New requests are then refused |
| `limits.command_seconds` | How long a command runs before you get `running` |
| `limits.answer_seconds` | How long you have to call `wait` or `stop` |
| `requests.waiting` | How many of your requests wait for the person at the host |
| `requests.max_waiting` | How many may wait |
| `requests.running` | The `id` of the command that runs now, or `null` |

The call only reads. It changes nothing.

## Errors

These are not answers to a request. The call itself is turned away. The body names the error:

```json
{"error":"unauthorized"}
```

| HTTP status | `error` | Meaning |
|---|---|---|
| 400 | `bad_request` | The body of a new request cannot be used: it is not JSON, it is too large, `kind` is not `command`, or `command` is empty |
| 401 | `unauthorized` | No key, a wrong key, a key whose session has ended, a second `hello`, or a missing or wrong caller token |
| 404 | `not_found` | The `id` in `wait` or `stop` is not a command that waits for that answer, or the path and method are not one of the five calls |

A 401 does not say which of its causes it was.

## A whole example

Start the two programs as in "What you need". Then, in a third terminal:

```
GW=http://127.0.0.1:8642
KEY=gw_4f9c…

HELLO=$(curl -sS -X POST "$GW/v1/hello" -H "Authorization: Bearer $KEY")
echo "$HELLO"
CALLER=$(echo "$HELLO" | sed 's/.*"caller_token":"\([^"]*\)".*/\1/')

curl -sS "$GW/v1/session" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER"

curl -sS -X POST "$GW/v1/requests" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER" \
  -d '{"kind":"command","command":"uname -s"}'
```

Replace `gw_4f9c…` with the key from your paste line. The last call waits. Go to the terminal of the cli-tool and press `y`. The call then ends with:

```json
{"state":"ended","how":"exit","exit_code":0,"output":"Linux\n","cut":false,"bytes_replaced":false}
```

To end the session, press Ctrl+C in the terminal of the cli-tool. Every call with that key then gets a 401.
