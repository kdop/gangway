# Gangway requirements

The hard requirements of the first version. Each one is a single sentence a test can check. The design and the reasons are in `blueprint.md` and `CLAUDE.md`.

## Rules

- Each requirement has an ID. An ID is never reused and never renumbered. A dropped requirement keeps its line, marked "dropped".
- Each requirement has at least one Go test. The test carries the ID in its name, for example `TestSES03_EndedSessionRefusesRequests`.
- One Go test lists every ID in this file that has no test. An empty list means every requirement is tested.
- The "Step" column names the step of `plan.md` that delivers the requirement. A step is done when the tests of its requirements pass.
- A requirement is added here before its code is written.

## GATE: approval

| ID | Requirement | Step |
|---|---|---|
| GATE-01 | A request must not run unless the user presses `y` at the cli-tool's prompt for that request. | 1 |
| GATE-02 | On `n`, nothing must run, and the AI must get "refused". | 1 |
| GATE-03 | A key other than `y` or `n` must not answer the prompt. | 1 |
| GATE-04 | A key pressed before the prompt is shown must not answer it. | 1 |
| GATE-05 | The prompt must show the exact request, and what runs on `y` must be exactly what was shown. | 1 |
| GATE-06 | Requests that arrive while another is in front must wait and be asked one at a time, in the order they came. | 1 |
| GATE-07 | No message from the controller or the AI must count as a yes. | 1 |
| GATE-08 | An approved command must run as the plain user who started the cli-tool. | 1 |
| GATE-09 | A file read must pass the same prompt as a command. | 5 |

## SHOW: nothing is hidden

| ID | Requirement | Step |
|---|---|---|
| SHOW-01 | The cli-tool's terminal must show each request and the answer given to it. | 1 |
| SHOW-02 | The cli-tool's terminal must show the output that is sent back and how the command ended. | 1 |

## SES: the session

| ID | Requirement | Step |
|---|---|---|
| SES-01 | A request must be refused when no live session has its key, and nothing must reach a host. | 3 |
| SES-02 | A Ctrl+C with nothing in front must end the session. | 3 |
| SES-03 | A session that has ended must never accept a request again. A new start must make a new session with a new key. | 3 |
| SES-04 | When the connection between the cli-tool and the controller is lost, the session must end and its key must die. | 3 |
| SES-05 | When a session ends by Ctrl+C or a lost connection, a running command must be stopped. | 3 |
| SES-06 | Neither the cli-tool nor the controller must write a session or its key to disk. | 3 |
| SES-07 | After a restart, the controller must have no live session. | 3 |
| SES-08 | When a session ends, the host must leave the Tailscale network. | 2 |

## KEY: the session key and the API

| ID | Requirement | Step |
|---|---|---|
| KEY-01 | The cli-tool must make a new random session key for every session. | 3 |
| KEY-02 | An API call with no key or a wrong key must be refused. | 3 |
| KEY-03 | The key must stop working the moment its session ends. | 3 |
| KEY-04 | The paste line must hold the skill's name, the controller's address and the key, in the form set in `ux.md`. | 3 |
| KEY-05 | On the first call with a key, the controller must hand the caller a token of its own. A later call with the key and without that token must be refused. | 4 |
| KEY-06 | Off localhost, the API must answer over HTTPS only. | 12 |
| KEY-07 | Many wrong keys from one address must get that address blocked for a set time. | 12 |
| KEY-08 | A file read must never return the Tailscale auth key file or the host token file. No setting must be able to switch this off. | 5 |

## LEN: session length

| ID | Requirement | Step |
|---|---|---|
| LEN-01 | A session must last 30 minutes when no other length is set. | 4 |
| LEN-02 | The user must be able to set another length at start. | 4 |
| LEN-03 | When the time is up, the cli-tool must ask at its terminal whether to extend, and `y` must give the session more time. | 4 |
| LEN-04 | No answer to the extend question within its set time must end the session. | 4 |
| LEN-05 | The whole session, extensions included, must never pass the admin maximum. At the maximum the session must end with no question. | 4 |
| LEN-06 | From the moment the time is up, new requests must be refused. | 4 |
| LEN-07 | A prompt that waits when the time is up must be refused, and nothing must run. | 4 |
| LEN-08 | A command that runs when the time is up must not be killed at that moment. It must get no more "wait more" and must be killed at its next time limit. | 6 |
| LEN-09 | No message from the AI or the controller must make a session longer. | 4 |

## LIM: limits on a command

| ID | Requirement | Step |
|---|---|---|
| LIM-01 | When a command passes the time limit, the AI must be told it is still running and asked to wait or stop. | 6 |
| LIM-02 | On "stop" the command must be killed. On "wait" it must run on until the next time limit. | 6 |
| LIM-03 | With no answer within the question's time limit and the command still running, the command must be killed. | 6 |
| LIM-04 | A command that ends by itself while the question waits must have its result sent as usual. | 6 |
| LIM-05 | When the output passes the size limit, the AI must get the first part and be asked to keep receiving or stop. | 6 |
| LIM-06 | The starting limits must be 60 seconds for a command, 100 KB of output and 60 seconds for the question, and each must be a setting. | 6 |
| LIM-07 | If the AI hangs up while a command runs, the command must be killed. | 6 |
| LIM-08 | One Ctrl+C while a command runs must kill the command and keep the session. | 6 |
| LIM-09 | One Ctrl+C at a waiting prompt must refuse that request and keep the session. | 1 |
| LIM-10 | Two Ctrl+C within a set short time must end the cli-tool from any state. | 1 |

## HOST: the cli-tool on the host

| ID | Requirement | Step |
|---|---|---|
| HOST-01 | The cli-tool must refuse to start as root, and say why. | 9 |
| HOST-02 | The cli-tool must refuse to start without a terminal. | 9 |
| HOST-03 | The cli-tool must listen on no port of the host. | 2 |
| HOST-04 | After `login`, a start must ask the user for nothing. | 2 |
| HOST-05 | The Tailscale auth key file must be readable by its owner only. | 2 |
| HOST-06 | The cli-tool must join Tailscale with no Tailscale install on the host and no root. | 2 |

## PAIR: pairing a host to a user

| ID | Requirement | Step |
|---|---|---|
| PAIR-01 | The controller must refuse a host that shows no valid host token, except for a pairing request. | 13 |
| PAIR-02 | A pairing code must work once. | 13 |
| PAIR-03 | A pairing code must stop working after a set time. | 13 |
| PAIR-04 | Before it accepts a pairing, the cli-tool must show the account that claimed the host and ask `y`/`n`. | 13 |
| PAIR-05 | On `n`, the host must stay unpaired and the code must be dead. | 13 |
| PAIR-06 | The host token file must be readable by its owner only. | 13 |
| PAIR-07 | The controller must store only a hash of a host token. | 13 |
| PAIR-08 | After a host is removed in the dashboard, its token must be refused. | 13 |
| PAIR-09 | A session must belong to the user the host is paired to. | 13 |

## CTL: the controller

| ID | Requirement | Step |
|---|---|---|
| CTL-01 | With no setting, the controller must answer on localhost only. | 1 |
| CTL-02 | The controller must refuse to answer on a public address when no login is set up. | 12 |
| CTL-03 | The dashboard must show nothing to a visitor who is not signed in. | 12 |
| CTL-04 | An account that is not on the list must be refused, even when the provider accepts it. | 12 |
| CTL-05 | The controller must store no password. | 12 |
| CTL-06 | A user must see and end only their own sessions. | 13 |
| CTL-07 | The dashboard must list the live sessions and their requests and answers as they happen. | 8 |
| CTL-08 | The end button in the dashboard must end the session. | 8 |
| CTL-09 | The dashboard must offer no way to approve a request. | 8 |
| CTL-10 | A host must reach the controller over Tailscale only. | 2 |

## SET: settings

| ID | Requirement | Step |
|---|---|---|
| SET-01 | A value from the user layer or a start option must never widen a bound set in `/etc/gangway/`. | 4 |
| SET-02 | The cli-tool must only read `/etc/gangway/`, never write it. | 4 |
| SET-03 | With no admin file, the user layer must be the top. | 4 |

## LOG: the log

| ID | Requirement | Step |
|---|---|---|
| LOG-01 | Every request and its answer must be written to the log on the host, with the time. | 7 |
| LOG-02 | The log must be kept in `~/.local/state/gangway/`. | 7 |

## Values not set yet

These requirements say "a set time" or give no number, because the design has none yet. Each needs a number before its test is written.

| Requirement | Missing value |
|---|---|
| KEY-01 | How many random bits the key has |
| KEY-07 | How many wrong keys, and how long the block lasts |
| LEN-04 | How long the extend question waits |
| LIM-10 | How close together the two Ctrl+C must be |
| PAIR-03 | How long a pairing code lives |

## Not here yet

Folder grants, "always allow", read-only mode and sudo are later stages. Their requirements are added when their design is settled.
