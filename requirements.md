# Gangway requirements and acceptance tests

The hard requirements of the first version, each with the acceptance tests that check it. A requirement is a single sentence. Its tests sit right under it. The design and the reasons are in `blueprint.md` and `CLAUDE.md`.

## Rules

- Each requirement has an ID. An ID is never reused and never renumbered. A dropped requirement keeps its line, marked "dropped".
- Each requirement has at least one acceptance test, and as many as it needs. A test's ID is the requirement's ID, a dot, and a number: `GATE-01.2` is the second test of `GATE-01`.
- Each acceptance test becomes a Go test that carries the ID in its name, for example `TestGATE01_2_NoKeyNoRun`.
- One Go test lists every test ID in this file that has no Go test. An empty list means everything here is tested.
- "Step" names the step of `plan.md` that delivers the requirement. A step is done when the tests of its requirements pass.
- A requirement and its tests are written here before the code.

## How to read a test

An acceptance test sees Gangway from outside. It calls the API as the AI would, presses keys as the user would, and looks at what comes back and at what the terminal shows.

- **Start.** The state before the test acts. "Live session" means: the cli-tool is connected to the controller, the AI holds the session key, and nothing is in front.
- **Action.** What the AI, the user or the network does.
- **Expect.** What must be true after. A test fails if any part is not.

To prove that a command did or did not run, a test asks for a command that leaves a mark, such as making an empty file, and then looks for the mark.

Where a test says a time "passes", the test sets that setting to a short value first. No test waits 30 minutes.

Where a test says the user presses a key at a prompt, `PROMPT_MIN_SHOW` has passed since the prompt was shown, unless the test says otherwise.

## The MVP

The MVP is set in `plan.md`. It delivers these 39 requirements:

| Group | Requirements |
|---|---|
| GATE | 01, 02, 03, 04, 05, 06, 07, 08, 10, 11, 12, 13, 14, 15 |
| SHOW | 01, 02, 03 |
| SES | 01, 02, 03, 05, 06, 07 |
| KEY | 01, 02, 03, 04 |
| LIM | 01, 02, 03, 04, 06, 07, 08, 09, 10 |
| HOST | 01, 02 |
| CTL | 01 |

Two of them are delivered in part:

- `SES-05`: there is no reconnect in the MVP, so a lost connection ends the session at once and stops the running command.
- `LIM-06`: `COMMAND_TIME_LIMIT` and `QUESTION_WAIT` only. `OUTPUT_SIZE_LIMIT` comes with the size cut.

## GATE: approval

### GATE-01

A request must not run unless the user presses `y` at the cli-tool's prompt for that request. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-01.1 | Live session | The AI asks for a command. The user presses `y` | The command runs. The AI gets its output and exit code |
| GATE-01.2 | Live session | The AI asks for a command. Nobody presses a key | The prompt waits. The command has not run |
| GATE-01.3 | A prompt waits | The connection to the controller is lost | The command has not run |
| GATE-01.4 | A prompt waits | The user ends the cli-tool | The command has not run |

### GATE-02

On `n`, nothing must run, and the AI must get "refused". Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-02.1 | A prompt waits | The user presses `n` | The command has not run. The AI gets "refused" |
| GATE-02.2 | A prompt waits | The user presses `n`. The AI asks for the same command again | A new prompt is shown. The first `n` does not answer it |

### GATE-03

The prompt must be answered by one key press, with no Enter: lower-case `y` or lower-case `n`. Every other key must be ignored. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-03.1 | A prompt waits | The user presses, one by one: Enter, space, `x`, `1`, Escape, `Y`, `N` | The prompt still waits after each key. The command has not run. The AI has no answer yet |
| GATE-03.2 | A prompt waits | The user presses `x`, then `y` | The command runs. The `x` did no harm |
| GATE-03.3 | A prompt waits | The user presses `y` and no Enter | The command runs at once |
| GATE-03.4 | A prompt waits. A second request waits behind it | The user types `yes` | The `y` answers the first prompt. The `e` and the `s` are ignored and do not answer the second |
| GATE-03.5 | A prompt waits. A second request waits behind it | The user types `no` | The `n` refuses the first. The `o` is ignored. The second prompt waits |

### GATE-04

A key pressed before the prompt is shown must not answer it. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-04.1 | Live session | The user presses `y`. Then the AI asks for a command | The prompt waits. The command has not run |
| GATE-04.2 | A command runs | The user presses `y`. The command ends. The AI asks for another command | The prompt for the second command waits. The second command has not run |
| GATE-04.3 | A prompt waits | The user presses `y` twice fast. The AI asks for another command | The first command runs. The prompt for the second waits |

### GATE-05

The prompt must show the exact request, and what runs on `y` must be exactly what was shown. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-05.1 | Live session | The AI asks for a command with arguments and quotes | The terminal shows the request letter for letter |
| GATE-05.2 | The prompt of GATE-05.1 waits | The user presses `y` | The output matches that exact command, not a changed one |
| GATE-05.3 | Live session | The AI asks for a command that holds a line break and a tab | The terminal shows a real line break and a real tab, and the rest letter for letter |
| GATE-05.4 | Live session | The AI asks for a command of many lines | The terminal shows every line before it asks `y`/`n` |

### GATE-06

Requests that arrive while another is in front must wait and be asked one at a time, in the order they came. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-06.1 | Live session | The AI asks for command A, then command B, before any answer | Only A's prompt is shown. B is not shown and has not run |
| GATE-06.2 | State after GATE-06.1 | The user presses `y` | A runs. Then B's prompt is shown. B has not run |
| GATE-06.3 | Live session | The AI asks for A, B and C. The user presses `n`, `y`, `n` | A did not run, B ran, C did not run. The AI gets three answers that match |

### GATE-07

No message from the controller or the AI must count as a yes. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-07.1 | A prompt waits | The controller sends the cli-tool a message that says the request is approved | The prompt still waits. The command has not run |
| GATE-07.2 | A prompt waits | The AI sends an API call that says the request is approved | The call is refused. The prompt still waits. The command has not run |
| GATE-07.3 | Live session | The controller sends a request marked as already approved | The cli-tool shows a prompt for it all the same. The command has not run |

### GATE-08

An approved command must run as the plain user who started the cli-tool. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-08.1 | Live session, the cli-tool started by user U | The AI asks who the current user is. The user presses `y` | The answer is U |

### GATE-09

A file read must pass the same prompt as a command. Step 5.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-09.1 | Live session | The AI asks to read a file. The user presses `y` | The AI gets the file's contents |
| GATE-09.2 | Live session | The AI asks to read a file. The user presses `n` | The AI gets "refused" and no part of the file |
| GATE-09.3 | Live session | The AI asks to read a file. Nobody presses a key | The prompt waits. The AI has no part of the file |

### GATE-10

A request that holds a control character other than a line break or a tab must be refused before any prompt. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-10.1 | Live session | The AI asks for a command that holds a carriage return | No prompt is shown. The command has not run. The AI gets "refused" with the reason |
| GATE-10.2 | Live session | The AI asks, one by one, for commands that hold: a backspace, an Escape, a colour code, a cursor code, a bell, a zero byte | Each is refused the same way |
| GATE-10.3 | Live session | The AI asks for a command that holds line breaks and tabs | The prompt is shown as usual, with real line breaks and tabs |
| GATE-10.4 | Live session | The AI asks for a command that holds a cursor code | The terminal shows that a request was refused. The cursor code itself never reaches the terminal |
| GATE-10.5 | Live session | The AI asks to read a file whose path holds an Escape | Refused the same way. The file is not read |
| GATE-10.6 | Live session | The AI asks for a command that holds the written form of a code, such as `\033[31m` or `\n`, made of visible characters only | The prompt is shown as usual, letter for letter |

### GATE-11

An approved command must run through the shell named by `COMMAND_SHELL`, so that pipes and redirection work. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-11.1 | Live session | The AI asks for two commands joined by a pipe. The user presses `y` | The AI gets the output of the second command, fed by the first |
| GATE-11.2 | Live session | The AI asks for a command that writes its output to a file with `>`. The user presses `y` | The file exists and holds that output |
| GATE-11.3 | Live session | The AI asks for two commands joined by `;`. The user presses `y` once | Both run, under that one approval. The prompt showed the whole line |
| GATE-11.4 | Live session | The AI asks for two commands joined by `;`. The user presses `n` | Neither runs |

### GATE-12

If the AI hangs up while a prompt waits, the prompt must be removed and nothing must run. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-12.1 | A prompt waits | The AI hangs up | The prompt is removed. The command has not run. The session lives on |
| GATE-12.2 | State after GATE-12.1 | The user presses `y` | Nothing runs. The key is ignored |
| GATE-12.3 | State after GATE-12.1 | The AI asks for the same command again | A new prompt is shown |

### GATE-13

No more than `MAX_WAITING_REQUESTS` requests must wait in line. A request past that must be refused at once. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-13.1 | Live session | The AI asks for `MAX_WAITING_REQUESTS` commands, with no answer given | All of them wait in line |
| GATE-13.2 | State after GATE-13.1 | The AI asks for one more | That one is refused at once, with the reason. It never gets a prompt |
| GATE-13.3 | State after GATE-13.2 | The user answers one prompt. The AI asks for one more | The new request is taken into the line |

### GATE-14

A key pressed within `PROMPT_MIN_SHOW` after a prompt is shown must not answer it. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-14.1 | Live session | The AI asks for a command. The user presses `y` before `PROMPT_MIN_SHOW` has passed | The prompt still waits. The command has not run |
| GATE-14.2 | State after GATE-14.1 | `PROMPT_MIN_SHOW` passes. The user presses `y` | The command runs |
| GATE-14.3 | A prompt waits for A. B waits behind it | The AI hangs up on A. The user presses `y` at once | The prompt for B waits. B has not run |
| GATE-14.4 | A prompt waits for A. B waits behind it | The user presses `n` twice, at once | A is refused. The prompt for B waits |
| GATE-14.5 | Live session | The AI asks for a command. The user presses Ctrl+C before `PROMPT_MIN_SHOW` has passed | The request is refused. Ctrl+C is not held back |

### GATE-15

One `y` must run its request once, and never a second time. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| GATE-15.1 | Live session | The AI asks for a command that adds one line to a file. The user presses `y` | The file holds one line |
| GATE-15.2 | Live session | The same request. The user presses `y` many times | The file holds one line |
| GATE-15.3 | Live session | The AI asks for the same command twice. The user presses `y` at the first prompt only | The file holds one line. The second prompt waits |

## SHOW: nothing is hidden

### SHOW-01

The cli-tool's terminal must show each request and the answer given to it. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| SHOW-01.1 | Live session | The AI asks for a command. The user presses `y` | The terminal shows the request and that it was approved |
| SHOW-01.2 | Live session | The AI asks for a command. The user presses `n` | The terminal shows the request and that it was refused |
| SHOW-01.3 | Live session | A request is refused with no prompt: once for a control character, once for a full line | Each time the terminal shows that a request was refused, and why |

### SHOW-02

The cli-tool's terminal must show the output that is sent back and how the command ended. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| SHOW-02.1 | Live session | An approved command prints two lines and ends with exit code 0 | The terminal shows both lines and exit code 0 |
| SHOW-02.2 | Live session | An approved command fails with exit code 3 | The terminal shows its output and exit code 3 |
| SHOW-02.3 | A command runs | The command is killed: once by Ctrl+C, once at a time limit | Each time the terminal shows that it was killed, and why |

### SHOW-03

Output shown on the cli-tool's terminal must hold no control character other than a line break or a tab. The AI must still get the output unchanged, except that bytes that are not text are replaced by a mark, and the answer says so. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| SHOW-03.1 | Live session | An approved command prints a cursor code and a colour code | Those codes never reach the terminal. The AI gets the output byte for byte |
| SHOW-03.2 | Live session | An approved command prints line breaks and tabs | The terminal shows them as they are |
| SHOW-03.3 | Live session | An approved file read returns a file that holds an Escape | No raw Escape reaches the terminal. The AI gets the file unchanged |
| SHOW-03.4 | Live session | An approved command prints bytes that are not text, such as the start of a picture file | The AI gets text in which each such byte is replaced by a mark. The answer says that bytes were replaced. The text around them is unchanged |
| SHOW-03.5 | Live session | An approved command prints the same bytes through `base64` | The AI gets the coded text unchanged. The answer does not say that bytes were replaced |

## SES: the session

### SES-01

A request must be refused when no live session has its key, and nothing must reach a host. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-01.1 | No session has been started | The AI calls the API with a made-up key | The call is refused. No host receives anything |
| SES-01.2 | A session was ended by the user | The AI calls the API with that session's key | The call is refused. No host receives anything |

### SES-02

A Ctrl+C with nothing in front must end the session. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-02.1 | Live session | The user presses Ctrl+C once | The cli-tool ends. The controller has no such session. The key is refused |

### SES-03

A session that has ended must never accept a request again. A new start must make a new session with a new key. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-03.1 | A session has ended | The AI sends a request with the old key | Refused. No prompt is shown anywhere |
| SES-03.2 | A session has ended | The user starts the cli-tool again | A new session with a key that differs from the old one. The old key is still refused |
| SES-03.3 | A session ended while requests waited in line | The user starts the cli-tool again | None of the old requests shows up |

### SES-04

When the connection between the cli-tool and the controller is lost, the cli-tool must try to connect again for `RECONNECT_WINDOW`. After it connects again in that time, the session must carry on with the same key. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-04.1 | Live session | The connection is cut, and comes back before `RECONNECT_WINDOW` has passed | The cli-tool connects again by itself. The terminal says the connection was lost and is back. The same key is accepted |
| SES-04.2 | State after SES-04.1 | The AI sends a request with the same key and caller token | A prompt is shown |
| SES-04.3 | Live session | The connection is cut and comes back three times, each time within `RECONNECT_WINDOW` | The session carries on each time |

### SES-05

When a session ends by Ctrl+C, or because the connection did not come back in time, a running command must be stopped. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-05.1 | A command runs | The user presses Ctrl+C twice within `DOUBLE_CTRL_C_WINDOW` | The command is stopped. The cli-tool ends |
| SES-05.2 | A command runs | The connection is cut and stays away past `RECONNECT_WINDOW` | The command is stopped. The session ends |

### SES-06

Neither the cli-tool nor the controller must write a session or its key to disk. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-06.1 | A live session that has served some requests | Search every file the cli-tool and the controller have written, the log too | The session key is in none of them. No file lists the session as live |
| SES-06.2 | That session has ended | The same search | The same result |

### SES-07

After a restart, the controller must have no live session. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-07.1 | Two live sessions | The controller is restarted | It has no live session. Both old keys are refused |
| SES-07.2 | State after SES-07.1 | Look at both cli-tools | Each has ended its session and says the controller went away |

### SES-08

When a session ends, the host must leave the Tailscale network. Step 2.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-08.1 | Live session | The session ends | The cli-tool has closed its Tailscale connection. The host no longer answers on the network |

### SES-09

When the connection has not come back within `RECONNECT_WINDOW`, the session must end and its key must die. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-09.1 | Live session | The connection is cut and stays away past `RECONNECT_WINDOW` | The cli-tool ends the session and says why. The controller refuses the key |
| SES-09.2 | State after SES-09.1 | The network comes back | The old session does not come back. The key is still refused |

### SES-10

While the connection is lost, a new request must be refused with the reason `host_away`, and what is already on the host must carry on. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-10.1 | The connection is lost, within `RECONNECT_WINDOW` | The AI sends a request | Refused with `host_away`. Nothing reaches the host |
| SES-10.2 | A prompt waits. The connection is lost | The user presses `y`. The command ends. The connection comes back | The command ran. The AI gets its result after the connection is back |
| SES-10.3 | A command runs. The connection is lost | The command ends by itself. The connection comes back | The command was not killed. The AI gets its result after the connection is back |
| SES-10.4 | A command runs. The connection is lost | The command reaches `COMMAND_TIME_LIMIT`, and no answer can come for `QUESTION_WAIT` | The command is killed, as with any unanswered question |

### SES-11

The controller must take a connection back into a session only when it shows the host token of that host and the hash of that session's key. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-11.1 | The connection is lost, within `RECONNECT_WINDOW` | A cli-tool connects with the right host token and a wrong key hash | Refused. The session is not handed to it |
| SES-11.2 | The same | A cli-tool connects with the right key hash and another host's token | Refused |
| SES-11.3 | The same | The real cli-tool connects with both | Taken back. The session carries on |

### SES-12

A session must never end because it is quiet. With no request at all, it must stay live until its time is up. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| SES-12.1 | Live session. The AI has never called | Wait until just before the session's time is up | The session is live. The key is accepted |
| SES-12.2 | Live session. The AI sent one request long ago | Wait until just before the session's time is up, then send a request | A prompt is shown |

## KEY: the session key and the API

### KEY-01

The cli-tool must make a new random session key of `SESSION_KEY_BITS` bits for every session. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-01.1 | The cli-tool has started | Read the key from the paste line | It holds `SESSION_KEY_BITS` bits |
| KEY-01.2 | Nothing | Start the cli-tool a hundred times and collect the keys | All keys differ |

### KEY-02

An API call with no key or a wrong key must be refused. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-02.1 | Live session | An API call with no key | Refused. No prompt on the host |
| KEY-02.2 | Live session | An API call with a wrong key | Refused. No prompt on the host |
| KEY-02.3 | Live session | An API call with the right key, once with one letter cut off, once with one letter added | Both refused |

### KEY-03

The key must stop working the moment its session ends. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-03.1 | Live session | The session ends. The AI calls with the key right after | Refused |

### KEY-04

The paste line must hold the skill's name, the controller's address and the key, in the form set in `ux.md`. Step 3.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-04.1 | The cli-tool has started | Read the line it printed | It has the form `/gangway <address> gw_<key>` |
| KEY-04.2 | The cli-tool has started | Take the address and the key from that line and call the API | The call is accepted |

### KEY-05

On the first call with a key, the controller must hand the caller a token of its own. A later call with the key and without that token must be refused. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-05.1 | Live session, the key not used yet | The first call with the key | The answer carries a caller token |
| KEY-05.2 | State after KEY-05.1 | A call with the key and the token | Accepted |
| KEY-05.3 | State after KEY-05.1 | A call with the key and no token | Refused |
| KEY-05.4 | State after KEY-05.1 | A call with the key and a wrong token | Refused |

### KEY-06

Off localhost, the API must answer over HTTPS only. Step 12.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-06.1 | The controller answers on an address that is not localhost | An API call over plain HTTP, with the right key | Not accepted |
| KEY-06.2 | The same | The same call over HTTPS | Accepted |
| KEY-06.3 | The controller answers on localhost | An API call over plain HTTP, with the right key | Accepted |

### KEY-07

More than `WRONG_KEY_LIMIT` wrong keys from one address within `WRONG_KEY_WINDOW` must get that address blocked for `WRONG_KEY_BLOCK`. Step 12.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-07.1 | Live session | One address sends one more wrong key than `WRONG_KEY_LIMIT`, within `WRONG_KEY_WINDOW`. Then it sends the right key | The right key is refused. The address is blocked |
| KEY-07.2 | State after KEY-07.1 | `WRONG_KEY_BLOCK` passes. The address sends the right key | Accepted |
| KEY-07.3 | Live session | One address sends the same number of wrong keys, spread over more than `WRONG_KEY_WINDOW`. Then the right key | Accepted. The address was never blocked |
| KEY-07.4 | One address is blocked | Another address sends the right key | Accepted |

### KEY-08

A file read must never return the Tailscale auth key file or the host token file. No setting must be able to switch this off. Step 5.

| Test | Start | Action | Expect |
|---|---|---|---|
| KEY-08.1 | Live session | The AI asks to read the auth key file | Refused with no prompt. No part of the file is returned |
| KEY-08.2 | Live session | The AI asks to read the host token file | The same |
| KEY-08.3 | Live session | The AI asks for either file by another road: through a link that points at it, and through a path with `..` in it | Both refused the same way |
| KEY-08.4 | Every setting and start option set to its widest | The AI asks to read the auth key file | Still refused |

## LEN: session length

### LEN-01

A session must last `SESSION_LENGTH` when no other length is set. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-01.1 | Live session, no length set | The AI sends a request just before `SESSION_LENGTH` has passed | A prompt is shown |
| LEN-01.2 | Live session, no length set | `SESSION_LENGTH` passes | The session's time is up: the question to extend is shown |

### LEN-02

The user must be able to set another length at start. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-02.1 | The cli-tool started with a length shorter than `SESSION_LENGTH` | That shorter time passes | The session's time is up |
| LEN-02.2 | The cli-tool started with a length longer than `SESSION_LENGTH` | `SESSION_LENGTH` passes | The session is still live |

### LEN-03

When the time is up, the cli-tool must ask at its terminal whether to extend, and `y` must give the session another stretch of the same length it started with. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-03.1 | The session's time is up | Look at the terminal | The question to extend is shown |
| LEN-03.2 | The question to extend is shown | The user presses `y` | The session is live again. A new request gets a prompt |
| LEN-03.3 | A session with no length set was extended once | Another `SESSION_LENGTH` passes | The session's time is up again, and not before |
| LEN-03.4 | A session started with a length of its own was extended once | That same length passes again | The session's time is up again, and not before |

### LEN-04

No answer to the extend question within `EXTEND_WAIT` must end the session. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-04.1 | The question to extend is shown | Nobody answers for `EXTEND_WAIT` | The session ends. The key is refused |
| LEN-04.2 | The question to extend is shown | The user presses `n` | The session ends at once |

### LEN-05

The whole session, extensions included, must never pass `SESSION_MAX`. At that point the session must end with no question. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-05.1 | `SESSION_MAX` is set. The user has extended the session each time | `SESSION_MAX` is reached | The session ends. No question to extend is shown |
| LEN-05.2 | `SESSION_MAX` is set shorter than `SESSION_LENGTH` | `SESSION_MAX` passes | The session has ended |

### LEN-06

From the moment the time is up, new requests must be refused. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-06.1 | The question to extend is shown | The AI sends a request | Refused. No prompt is shown |

### LEN-07

A prompt that waits when the time is up must be refused, and nothing must run. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-07.1 | A prompt waits | The session's time runs out | The prompt is removed. The AI gets "refused". The command has not run. Then the question to extend is shown |

### LEN-08

A command that runs when the time is up must not be killed at that moment. It must get no more "wait more" and must be killed at its next time limit. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-08.1 | A command runs | The session's time runs out | The command is still running |
| LEN-08.2 | State after LEN-08.1 | The command reaches `COMMAND_TIME_LIMIT` | It is killed. The AI is not asked to wait or stop |
| LEN-08.3 | A command runs. The session's time runs out | The command ends by itself before its time limit | Its result is sent as usual. Then the question to extend is shown |

### LEN-09

No message from the AI or the controller must make a session longer. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| LEN-09.1 | Live session | The AI sends an API call that asks for more time | Refused. The end time has not moved |
| LEN-09.2 | Live session | The controller sends the cli-tool a message to extend | Ignored. The end time has not moved |
| LEN-09.3 | The question to extend is shown | The controller sends the cli-tool a "yes" | The question still waits |

## LIM: limits on a command

### LIM-01

When a command has run for `COMMAND_TIME_LIMIT`, the AI must be told it is still running and asked to wait or stop. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-01.1 | An approved command that runs longer than `COMMAND_TIME_LIMIT` | `COMMAND_TIME_LIMIT` passes | The AI gets "still running" and the question. The command is still running |
| LIM-01.2 | An approved command that ends before `COMMAND_TIME_LIMIT` | The command ends | The AI gets the result and no question |

### LIM-02

On "stop" the command must be killed. On "wait" it must run on for another `COMMAND_TIME_LIMIT`. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-02.1 | The wait-or-stop question is asked | The AI answers "stop" | The command is killed. The AI is told so and gets the output so far |
| LIM-02.2 | The wait-or-stop question is asked | The AI answers "wait" | The command runs on. After another `COMMAND_TIME_LIMIT` the question comes again |
| LIM-02.3 | The AI has answered "wait" | The command ends by itself | The AI gets the whole result |

### LIM-03

With no answer within `QUESTION_WAIT` and the command still running, the command must be killed. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-03.1 | The wait-or-stop question is asked | No answer comes for `QUESTION_WAIT` | The command is killed |

### LIM-04

A command that ends by itself while the question waits must have its result sent as usual. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-04.1 | The wait-or-stop question is asked | The command ends by itself before any answer | The AI gets the result on its next call. Nothing is killed |

### LIM-05

When the output passes `OUTPUT_SIZE_LIMIT`, the cli-tool must stop sending output, must not kill the command, and must end what it sends with a note that the output was cut. No question must be asked. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-05.1 | An approved command that prints more than `OUTPUT_SIZE_LIMIT` and then ends | The command runs | The AI gets the first `OUTPUT_SIZE_LIMIT` of output, then a note that the output was cut, and the exit code. The AI is asked nothing |
| LIM-05.2 | An approved command that prints more than `OUTPUT_SIZE_LIMIT` and then leaves a mark | The command runs | The mark is there: the command ran to its end and was not killed |
| LIM-05.3 | An approved command that prints less than `OUTPUT_SIZE_LIMIT` | The command ends | The AI gets the whole output and no note |
| LIM-05.4 | An approved command that prints without end | `COMMAND_TIME_LIMIT` passes | The output was cut at `OUTPUT_SIZE_LIMIT`. The time limit acts as usual: the AI is asked to wait or stop |

### LIM-06

`COMMAND_TIME_LIMIT`, `OUTPUT_SIZE_LIMIT` and `QUESTION_WAIT` must each be a setting, with the starting value in the "Settings" table. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-06.1 | No settings file | Run LIM-01.1, LIM-03.1 and LIM-05.1 | Each limit acts at its starting value |
| LIM-06.2 | Each of the three set to another value in the user's settings | Run the same three | Each limit acts at the new value |

### LIM-07

If the AI hangs up while a command runs, the command must be killed. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-07.1 | A command runs | The AI hangs up | The command is killed. The terminal says why. The session lives on |

### LIM-08

One Ctrl+C while a command runs must kill the command and keep the session. Step 6.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-08.1 | A command runs | The user presses Ctrl+C once | The command is killed. The AI is told the user stopped it. The session lives on |
| LIM-08.2 | State after LIM-08.1 | The AI sends a new request | A prompt is shown |

### LIM-09

One Ctrl+C at a waiting prompt must refuse that request and keep the session. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-09.1 | A prompt waits | The user presses Ctrl+C once | The AI gets "refused". The command has not run. The session lives on |
| LIM-09.2 | A prompt waits. A second request waits behind it | The user presses Ctrl+C once | The first is refused. The prompt for the second is shown |

### LIM-10

Two Ctrl+C within `DOUBLE_CTRL_C_WINDOW` must end the cli-tool from any state. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| LIM-10.1 | A prompt waits | The user presses Ctrl+C twice within `DOUBLE_CTRL_C_WINDOW` | The cli-tool ends. The command has not run |
| LIM-10.2 | A command runs | The same two presses | The command is stopped. The cli-tool ends |
| LIM-10.3 | A prompt waits | The user presses Ctrl+C. The AI sends a new request at once. The user presses Ctrl+C again, within `DOUBLE_CTRL_C_WINDOW` | The cli-tool ends. The new request has not run |
| LIM-10.4 | A prompt waits. A second request waits behind it | The user presses Ctrl+C twice, further apart than `DOUBLE_CTRL_C_WINDOW` | Each press refuses one request. The cli-tool does not end |

## HOST: the cli-tool on the host

### HOST-01

The cli-tool must refuse to start as root, and say why. Step 9.

| Test | Start | Action | Expect |
|---|---|---|---|
| HOST-01.1 | A root shell | Start the cli-tool | It prints the reason and exits. It has made no connection |
| HOST-01.2 | A plain user's shell | Start the cli-tool with `sudo` | The same |

### HOST-02

The cli-tool must refuse to start without a terminal. Step 9.

| Test | Start | Action | Expect |
|---|---|---|---|
| HOST-02.1 | Nothing | Start the cli-tool with its input not tied to a terminal | It prints the reason and exits |
| HOST-02.2 | Nothing | Start the cli-tool in the background | The same |

### HOST-03

The cli-tool must listen on no port of the host. Step 2.

| Test | Start | Action | Expect |
|---|---|---|---|
| HOST-03.1 | Live session | List the ports the host listens on | None belongs to the cli-tool |

### HOST-04

After `login`, a start must ask the user for nothing. Step 2.

| Test | Start | Action | Expect |
|---|---|---|---|
| HOST-04.1 | `login` has been done | Start the cli-tool and type nothing | It reaches a live session and prints the paste line |
| HOST-04.2 | State after a first session has ended | Start the cli-tool again and type nothing | The same |

### HOST-05

The Tailscale auth key file must be readable by its owner only. Step 2.

| Test | Start | Action | Expect |
|---|---|---|---|
| HOST-05.1 | `login` has been done | Look at the rights of the auth key file | Only its owner can read or write it |

### HOST-06

The cli-tool must join Tailscale with no Tailscale install on the host and no root. Step 2.

| Test | Start | Action | Expect |
|---|---|---|---|
| HOST-06.1 | A host with no Tailscale installed, and a plain user | Do `login`, then start the cli-tool | A live session, reached over Tailscale |

## PAIR: pairing a host to a user

### PAIR-01

The controller must refuse a host that shows no valid host token, except for a pairing request. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-01.1 | A host that was never paired | Start the cli-tool | The controller refuses it. There is no session |
| PAIR-01.2 | A host with a made-up token | Start the cli-tool | The same |
| PAIR-01.3 | A host that was never paired | The cli-tool asks for a pairing code | The controller gives one |

### PAIR-02

A pairing code must work once. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-02.1 | A code was used for a finished pairing | A user types the same code again | Refused |

### PAIR-03

A pairing code must stop working after `PAIRING_CODE_LIFE`. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-03.1 | A code is shown | `PAIRING_CODE_LIFE` passes. The user types the code | Refused |
| PAIR-03.2 | A code is shown | The user types the code before `PAIRING_CODE_LIFE` has passed | Taken. The cli-tool asks `y`/`n` |

### PAIR-04

Before it accepts a pairing, the cli-tool must show the account that claimed the host and ask `y`/`n`. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-04.1 | A code is shown | User A, signed in, types the code | The cli-tool shows account A and asks `y`/`n`. No host token has been given yet |
| PAIR-04.2 | State after PAIR-04.1 | The user at the host presses `y` | The cli-tool gets a host token. The dashboard lists the host under A |

### PAIR-05

On `n`, the host must stay unpaired and the code must be dead. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-05.1 | State after PAIR-04.1 | The user at the host presses `n` | No host token is given. The dashboard does not list the host |
| PAIR-05.2 | State after PAIR-05.1 | A user types the same code again | Refused |

### PAIR-06

The host token file must be readable by its owner only. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-06.1 | A paired host | Look at the rights of the host token file | Only its owner can read or write it |

### PAIR-07

The controller must store only a hash of a host token. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-07.1 | A paired host | Search every file the controller has written for the host token | Not found |

### PAIR-08

After a host is removed in the dashboard, its token must be refused. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-08.1 | A paired host, removed in the dashboard | Start the cli-tool | The controller refuses it. There is no session |

### PAIR-09

A session must belong to the user the host is paired to. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-09.1 | A host paired to user A | Start a session | The dashboard lists the session under A and under nobody else |

### PAIR-10

With no answer to the pairing question by the time `PAIRING_CODE_LIFE` has passed, the host must stay unpaired and the code must be dead. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-10.1 | The cli-tool asks `y`/`n` for an account that typed the code | Nobody answers until `PAIRING_CODE_LIFE` has passed since the code was made | The question goes away. No host token is given. The dashboard does not list the host |
| PAIR-10.2 | State after PAIR-10.1 | The user at the host presses `y` | Nothing happens. The host is still unpaired |
| PAIR-10.3 | State after PAIR-10.1 | A user types the same code again | Refused |

### PAIR-11

When a host is removed in the dashboard, its live session must end at once. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| PAIR-11.1 | A paired host with a live session | The user removes the host in the dashboard | The session ends at once. The cli-tool says the host was removed. The session key is refused |
| PAIR-11.2 | A paired host with a prompt waiting | The user removes the host in the dashboard | The session ends. The command has not run |
| PAIR-11.3 | State after PAIR-11.1 | Start the cli-tool again | The controller refuses it, until the host is paired again |

## CTL: the controller

### CTL-01

With no setting, the controller must answer on localhost only. Step 1.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-01.1 | The controller started with no setting | Call the API from the same machine | Answered |
| CTL-01.2 | The same | Call the API from another machine | No answer |

### CTL-02

The controller must refuse to answer on a public address when no login is set up. Step 12.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-02.1 | No login is set up | Start the controller set to a public address | It prints the reason and does not start |
| CTL-02.2 | A login is set up | The same | It starts |

### CTL-03

The dashboard must show nothing to a visitor who is not signed in. Step 12.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-03.1 | Not signed in | Open each page of the dashboard: sessions, log, hosts | No data is shown. The visitor is sent to sign in |
| CTL-03.2 | Not signed in | Send the "end session" action straight to the controller | Refused. The session lives on |

### CTL-04

An account that is not on the list must be refused, even when the provider accepts it. Step 12.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-04.1 | An account that is on the list | Sign in | Signed in |
| CTL-04.2 | A real account at the provider that is not on the list | Sign in | Refused. No data is shown |

### CTL-05

The controller must store no password. Step 12.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-05.1 | Several users have signed in | Search every file the controller has written | No password and no hash of one. The controller has no place to type a password |

### CTL-06

A user must see and end only their own sessions. Step 13.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-06.1 | Users A and B each have a live session | A opens the dashboard | A sees A's session and not B's |
| CTL-06.2 | The same | A sends the "end session" action for B's session | Refused. B's session lives on |

### CTL-07

The dashboard must list the live sessions and their requests and answers as they happen. Step 8.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-07.1 | The dashboard is open | A session starts | The session shows up with no reload |
| CTL-07.2 | The dashboard is open on a live session | The AI sends a request. The user answers it | The request and then the answer show up as they happen |

### CTL-08

The end button in the dashboard must end the session. Step 8.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-08.1 | Live session | Press the end button | The session ends. The cli-tool says it was ended from the dashboard. The key is refused |

### CTL-09

The dashboard must offer no way to approve a request. Step 8.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-09.1 | A prompt waits | Go through every page and every action of the dashboard | None approves the request. The prompt still waits |

### CTL-10

A host must reach the controller over Tailscale only. Step 2.

| Test | Start | Action | Expect |
|---|---|---|---|
| CTL-10.1 | The controller is running | A cli-tool tries to connect as a host over a road that is not Tailscale | Refused |

## SET: settings

### SET-01

A value from the user layer or a start option must never widen a bound set in `/etc/gangway/`. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| SET-01.1 | `SESSION_MAX` is 20 minutes in `/etc/gangway/`. The user's settings ask for 60 | Start a session | The session does not pass 20 minutes |
| SET-01.2 | The same bound. A start option asks for 60 minutes | Start a session | The same |
| SET-01.3 | The same bound. The user asks for 10 minutes | Start a session | The session's time is up at 10 minutes |

### SET-02

The cli-tool must only read `/etc/gangway/`, never write it. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| SET-02.1 | `/etc/gangway/` holds a file | Do `login`, run a whole session, end it | The folder and the file are unchanged. The cli-tool never opened them for writing |

### SET-03

With no admin file, the user layer must be the top. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| SET-03.1 | No `/etc/gangway/`. The user's settings ask for 2 hours | Start a session | The session lasts 2 hours. No maximum cuts it |

### SET-04

When a value asked for is wider than an admin bound, the cli-tool must start with the bound and tell the user. Step 4.

| Test | Start | Action | Expect |
|---|---|---|---|
| SET-04.1 | `SESSION_MAX` is 20 minutes. A start option asks for 60 | Start a session | The session starts with 20 minutes. The terminal says that 60 is above the maximum and was cut to 20 |
| SET-04.2 | `SESSION_MAX` is 20 minutes. The user's settings ask for 60 | Start a session | The same |
| SET-04.3 | `SESSION_MAX` is 20 minutes. The user asks for 10 | Start a session | The session starts with 10 minutes. No such message is shown |

## LOG: the log

### LOG-01

Every request and its answer must be written to the log on the host, with the time. Step 7.

| Test | Start | Action | Expect |
|---|---|---|---|
| LOG-01.1 | A session with one request approved, one refused with `n`, and one refused with no prompt | Read the log | All three are there, each with its answer and its time |
| LOG-01.2 | A session where one command was killed at a limit and one by Ctrl+C | Read the log | Both are there, each with how it ended |

### LOG-02

The log must be kept in `~/.local/state/gangway/`. Step 7.

| Test | Start | Action | Expect |
|---|---|---|---|
| LOG-02.1 | A session has ended | Look in `~/.local/state/gangway/` | The log is there, and in no other place |

### LOG-03

The log must hold the output of each command and file read, as it was sent to the AI. Step 7.

| Test | Start | Action | Expect |
|---|---|---|---|
| LOG-03.1 | A session where an approved command printed two lines | Read the log | Both lines are there, with the request |
| LOG-03.2 | A session where a command printed more than `OUTPUT_SIZE_LIMIT` | Read the log | The log holds the first `OUTPUT_SIZE_LIMIT` and the note that the output was cut, and no more |
| LOG-03.3 | A session where a request was refused | Read the log | The request is there with "refused" and no output |

### LOG-04

The log must be readable by its owner only. Step 7.

| Test | Start | Action | Expect |
|---|---|---|---|
| LOG-04.1 | A session has ended | Look at the rights of the log file and its folder | Only the owner can read or write them |

### LOG-05

When the log file passes `LOG_FILE_SIZE`, the cli-tool must start a new log file, and must keep no more than `LOG_FILES_KEPT` old ones, deleting the oldest first. Step 7.

| Test | Start | Action | Expect |
|---|---|---|---|
| LOG-05.1 | The log file is just under `LOG_FILE_SIZE` | A request is logged that takes it past the size | A new log file is started. The old one is kept under another name. No entry is lost or cut in half |
| LOG-05.2 | `LOG_FILES_KEPT` old log files exist | The log file passes `LOG_FILE_SIZE` again | The oldest file is deleted. There are still `LOG_FILES_KEPT` old files |
| LOG-05.3 | Many sessions have filled the log many times over | Add up the size of all log files | It is never more than `LOG_FILE_SIZE` times `LOG_FILES_KEPT` plus one |
| LOG-05.4 | A new log file was just started | Look at the rights of every log file | Only the owner can read or write them |

## Settings

A name in upper case is a setting. The requirements use the name, never the number, so a changed value changes no requirement. The values here are the starting values.

| Setting | Starting value | Meaning | Used by |
|---|---|---|---|
| `SESSION_KEY_BITS` | 256 | Random bits in a session key | KEY-01 |
| `WRONG_KEY_LIMIT` | 10 | Wrong keys from one address before it is blocked | KEY-07 |
| `WRONG_KEY_WINDOW` | 1 minute | The time in which those wrong keys are counted | KEY-07 |
| `WRONG_KEY_BLOCK` | 5 minutes | How long a blocked address stays blocked | KEY-07 |
| `SESSION_LENGTH` | 30 minutes | How long a session lasts when no other length is set | LEN-01 |
| `SESSION_MAX` | None | The longest a session may last, extensions included. Set in `/etc/gangway/` only | LEN-05 |
| `EXTEND_WAIT` | 60 seconds | How long the question to extend waits for an answer | LEN-04 |
| `MAX_WAITING_REQUESTS` | 20 | How many requests may wait in line for a prompt | GATE-13 |
| `PROMPT_MIN_SHOW` | 0.5 seconds | How long a prompt must be on screen before a key can answer it | GATE-14 |
| `COMMAND_SHELL` | `/bin/sh` | The shell that runs an approved command | GATE-11 |
| `COMMAND_TIME_LIMIT` | 60 seconds | How long a command runs before the AI is asked to wait or stop | LIM-01, LIM-02 |
| `QUESTION_WAIT` | 60 seconds | How long the wait-or-stop question waits for the AI | LIM-03 |
| `OUTPUT_SIZE_LIMIT` | 100 KB | How much output of one command is sent. The rest is cut | LIM-05 |
| `DOUBLE_CTRL_C_WINDOW` | 2 seconds | Two Ctrl+C within this time end the cli-tool | LIM-10 |
| `PAIRING_CODE_LIFE` | 5 minutes | How long a pairing code works | PAIR-03, PAIR-10 |
| `LINK_TIMEOUT` | 30 seconds | How long the connection may be silent before it counts as lost | SES-04 |
| `RECONNECT_WINDOW` | 2 minutes | How long the cli-tool tries to connect again before the session ends | SES-04, SES-09 |
| `LOG_FILE_SIZE` | 10 MB | How big one log file may get before a new one is started | LOG-05 |
| `LOG_FILES_KEPT` | 5 | How many old log files are kept | LOG-05 |

## Not here yet

Folder grants, "always allow", read-only mode and sudo are later stages. Their requirements are added when their design is settled.
