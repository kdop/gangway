# Gangway detailed design and unit tests

The inside of each piece, with the unit tests that check it. A piece is added here just before it is built. The pieces and how they fit are in `architecture.md`.

A unit test runs one piece alone. The pieces around it are fakes that record what they were asked.

Each unit test names the requirements it helps to prove. The Go test carries the same line in a comment, and a Go test fails when the two differ or when an ID is not in `requirements.md`.

## `gate`

The `gate` shows one request and takes the `y` or `n`. It is the only way to the `runner`.

### What it holds

- Its state.
- The request in front, if there is one.
- The time the prompt in front was shown.

### What it talks to

| Piece | The `gate` asks it to |
|---|---|
| `line` | Give the oldest waiting request |
| `terminal` | Show the prompt, and show the answer |
| `connection` | Send `started` or `refused` |
| `runner` | Run an approved request |
| The clock | Tell the time |

### States

| State | Meaning |
|---|---|
| `Free` | Nothing is in front |
| `Asking` | A prompt is on the screen and waits for a key |
| `Busy` | An approved request is with the `runner` |
| `Ended` | The session is over. Final |

```mermaid
stateDiagram-v2
  [*] --> Free
  Free --> Asking: the line has a request
  Asking --> Busy: y
  Asking --> Free: n, Ctrl+C, cancel, or the time is up
  Asking --> Asking: any other key
  Busy --> Free: the runner is done
  Free --> Ended: the session ends
  Asking --> Ended: the session ends
  Busy --> Ended: the session ends
  Ended --> [*]
```

### Events

| Event | Comes from |
|---|---|
| `ready` | `line`: a request waits. Sent when a request arrives, and asked for again each time the `gate` becomes `Free` |
| `key` | `terminal`: one key press |
| `ctrl_c` | `terminal`: one Ctrl+C |
| `cancel` | `connection`: the AI hung up on the request with this `id` |
| `done` | `runner`: the request it was given is over |
| `time_up` | `session`: the session's time is up |
| `end` | `session`: the session ends, for any reason |

### The table

Every state meets every event. An empty "Does" means the event is dropped.

| State | Event | Does | Next |
|---|---|---|---|
| `Free` | `ready` | Takes the oldest request from `line`. Shows the prompt. Notes the time | `Asking` |
| `Free` | `key` | | `Free` |
| `Free` | `ctrl_c` | Not the gate's. `session` ends the session | `Free` |
| `Free` | `cancel` | Not the gate's. `line` removes the request if it waits | `Free` |
| `Free` | `done` | | `Free` |
| `Free` | `time_up` | | `Free` |
| `Free` | `end` | | `Ended` |
| `Asking` | `ready` | | `Asking` |
| `Asking` | any `key`, less than `PROMPT_MIN_SHOW` after the prompt was shown | | `Asking` |
| `Asking` | `key` `y` | Shows the answer. Sends `started`. Hands the request to `runner` | `Busy` |
| `Asking` | `key` `n` | Shows the answer. Sends `refused` with `user` | `Free` |
| `Asking` | any other `key` | | `Asking` |
| `Asking` | `ctrl_c` | Shows the answer. Sends `refused` with `user` | `Free` |
| `Asking` | `cancel`, the `id` in front | Shows that the AI left. Sends `refused` with `ai_gone` | `Free` |
| `Asking` | `cancel`, another `id` | Not the gate's | `Asking` |
| `Asking` | `done` | | `Asking` |
| `Asking` | `time_up` | Shows that the time is up. Sends `refused` with `time_up`. Takes no next request | `Free` |
| `Asking` | `end` | Sends `refused` with `session_ended` | `Ended` |
| `Busy` | `ready` | | `Busy` |
| `Busy` | `key` | | `Busy` |
| `Busy` | `ctrl_c` | Not the gate's. `runner` kills the command | `Busy` |
| `Busy` | `cancel` | Not the gate's. `runner` kills the command if the `id` is its own | `Busy` |
| `Busy` | `done` | | `Free` |
| `Busy` | `time_up` | Not the gate's. `runner` gives the command no more time | `Busy` |
| `Busy` | `end` | Not the gate's. `runner` stops the command | `Ended` |
| `Ended` | any | | `Ended` |

Each time the `gate` becomes `Free`, it asks `line` whether a request waits. If one does, that is a `ready`. The one exception is `time_up`: the `gate` then asks for nothing, because nothing new may be shown until the user extends.

### Rules the code must keep

- **One way to the runner.** The `runner` starts a request only when it is handed an approval. Only the `gate` can make an approval, and it makes one in a single place: `Asking` with `key` `y`. No other code can build one. (`GATE-01`, `GATE-07`)
- **What was shown is what runs.** The approval carries the request itself, the same value the prompt was drawn from. The `runner` runs what is in the approval and takes the request from nowhere else. (`GATE-05`)
- **An approval works once.** It names one request, and the `runner` refuses it a second time. (`GATE-15`)
- **A key is never kept.** A `key` that does not answer a prompt is dropped at once. Nothing stores it for a later prompt. (`GATE-04`)
- **Exactly two keys answer.** The key must be the single byte `y` or the single byte `n`. Capitals, Enter, and every key that sends more than one byte are other keys. (`GATE-03`)
- **A fresh prompt takes no key.** A `key` that comes less than `PROMPT_MIN_SHOW` after the prompt was shown is dropped, `y` and `n` too. `ctrl_c` is not held back: it can only refuse. (`GATE-14`)
- **One answer for each request.** Every request the `gate` takes ends with one `started` or one `refused` from it, never both and never none.
- **The time being up can only refuse.** `time_up` refuses the prompt in front and approves nothing. (`LEN-07`)

### Unit tests

Start: a `gate` with a fake `line`, `terminal`, `connection` and `runner`, and a clock the test moves. In a test that starts in `Asking`, `PROMPT_MIN_SHOW` has passed unless the test says otherwise.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-GATE-01 | `GATE-01`, `GATE-05` | `Free`, one request in the line | `ready` | `Asking`. The prompt holds the request's exact text. The runner was not called |
| UNIT-GATE-02 | `GATE-01`, `GATE-05` | `Asking` | `key` `y` | `Busy`. `started` was sent. The runner got an approval that holds the same request as the prompt |
| UNIT-GATE-03 | `GATE-02` | `Asking` | `key` `n` | `Free`. `refused` with `user`. The runner was not called |
| UNIT-GATE-04 | `GATE-03` | `Asking` | In turn: `Y`, `N`, Enter, space, Escape, an arrow key, `x` | Still `Asking` after each one. Nothing was shown, sent or run |
| UNIT-GATE-05 | `LIM-09` | `Asking` | `ctrl_c` | `Free`. `refused` with `user`. The runner was not called |
| UNIT-GATE-06 | `GATE-12` | `Asking`, request A in front | `cancel` for A | `Free`. `refused` with `ai_gone`. The runner was not called |
| UNIT-GATE-07 | `GATE-12` | `Asking`, request A in front | `cancel` for B | Still `Asking`, with A in front. Nothing was sent |
| UNIT-GATE-08 | `GATE-01` | `Asking` | `end` | `Ended`. `refused` with `session_ended`. The runner was not called |
| UNIT-GATE-09 | `GATE-04` | `Free`, the line is empty | `key` `y`, then a request arrives and `ready` | `Asking`. The early `y` did not answer it |
| UNIT-GATE-10 | `GATE-04` | `Busy` | `key` `y`, then `done`, with a second request in the line | `Asking` for the second request. The `y` did not answer it |
| UNIT-GATE-11 | `GATE-06` | `Busy`, a second request in the line | `ready` | Still `Busy`. No second prompt. The line still holds the request |
| UNIT-GATE-12 | `GATE-02`, `GATE-06`, `GATE-14` | `Asking`, a second request in the line | `key` `n`, twice with no time between | `refused` for the first, then `Asking` for the second. The second `n` did not answer it |
| UNIT-GATE-13 | `GATE-06` | `Busy`, the line is empty | `done` | `Free`. No prompt |
| UNIT-GATE-14 | `SES-03` | `Ended` | Each event in turn | Still `Ended`. Nothing was shown, sent or run |
| UNIT-GATE-15 | `GATE-01`, `GATE-07` | Each state in turn | Each event in turn, except `key` `y` in `Asking` | The runner was never called |
| UNIT-GATE-16 | `GATE-01`, `GATE-05`, `GATE-07` | `Free` | A long run of events picked at random | The runner was called exactly as many times as `key` `y` came in `Asking`, each time with the request that was in front. Every request taken got one `started` or one `refused` |
| UNIT-GATE-17 | `GATE-15` | An approval that the runner has used | The same approval is handed to the runner again | The runner refuses it. Nothing runs |
| UNIT-GATE-18 | `GATE-14` | `Asking`, the prompt just shown | `key` `y`, then `key` `n`, with no time passed | Still `Asking`. Nothing was sent or run |
| UNIT-GATE-19 | `GATE-14` | State after UNIT-GATE-18 | The clock moves by `PROMPT_MIN_SHOW`. `key` `y` | `Busy`. The runner got the approval |
| UNIT-GATE-20 | `GATE-14`, `LIM-09` | `Asking`, the prompt just shown | `ctrl_c`, with no time passed | `Free`. `refused` with `user` |
| UNIT-GATE-21 | `GATE-12`, `GATE-14` | `Asking` for A, B in the line | `cancel` for A, then `key` `y` with no time passed | `Asking` for B. The runner was not called |
| UNIT-GATE-22 | `LEN-07` | `Asking` for A, B in the line | `time_up`, then `key` `y` | `Free`. `refused` with `time_up` for A, and the screen was told the time is up. B is still in the line and got no prompt. The runner was not called |
| UNIT-GATE-23 | `LEN-07`, `LEN-08` | `Free`, and again `Busy` | `time_up` | The same state. Nothing was shown or sent |

## `checks`

`checks` looks at a request before anything else does, and refuses it or lets it pass. It holds nothing and has no states: the same request always gets the same answer.

### What it refuses

A request is refused with the reason `control_character` when its text holds any of these:

| What | Which characters |
|---|---|
| A control character of the first set | Codes 0 to 31, except the line break (10) and the tab (9) |
| Delete | Code 127 |
| A control character of the second set | Codes 128 to 159 |
| Bytes that are not text | Anything that is not valid UTF-8 |
| A character that changes the reading order | The marks that turn text right to left or back, such as codes 8234 to 8238 and 8294 to 8297 |
| An invisible character | The zero-width space and joiners, the soft hyphen, the byte order mark, the invisible tag characters, and the line and paragraph separators (8232, 8233) |

In Unicode terms the last two rows are the whole "format" class, plus the two separators. The code refuses the class, not a list of its own, so a new character of that class is refused too.

Everything else passes: letters of any language, signs, spaces, line breaks and tabs.

The cost: an emoji built from joined parts, such as a family, holds an invisible joiner and is refused. A plain emoji passes.

### Rules the code must keep

- **The whole text is read.** One bad character anywhere refuses the request: at the start, in the middle or at the end.
- **The carriage return is refused.** It moves the cursor to the start of the line, so text after it can cover the text before it.
- **The second set is refused too.** Some terminals read code 155 as the start of a cursor code, the same as Escape followed by `[`.
- **What the prompt shows must be what runs.** A mark that turns the reading order around makes the screen show the characters in another order than the shell gets them. An invisible character makes two different commands look the same. (`GATE-16`)
- **The text is never changed.** `checks` says yes or no. It does not clean a request and pass it on, because the user must approve what the AI sent, not a repaired form of it.
- **The refused text never reaches the screen.** `checks` gives back the reason only.

File reads and the protected files are not in the MVP. They add a second check here when they are built.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-CHECKS-01 | `GATE-10` | | A plain command with a pipe and a redirection | Passes |
| UNIT-CHECKS-02 | `GATE-10` | | A command of three lines with tabs | Passes |
| UNIT-CHECKS-03 | `GATE-10` | | In turn, a command that holds each code from 0 to 31, except 9 and 10 | Each is refused with `control_character` |
| UNIT-CHECKS-04 | `GATE-10` | | A command that holds code 127 | Refused |
| UNIT-CHECKS-05 | `GATE-10` | | In turn, a command that holds each code from 128 to 159 | Each is refused |
| UNIT-CHECKS-06 | `GATE-10` | | Commands that hold the written forms `\033[31m`, `\n`, `\r` and `\x1b`, made of visible characters only | Each passes |
| UNIT-CHECKS-07 | `GATE-10` | | A command that holds bytes that are not valid UTF-8 | Refused |
| UNIT-CHECKS-08 | `GATE-10`, `GATE-16` | | Commands with Greek letters, accents, an emoji and a non-breaking space | Each passes |
| UNIT-CHECKS-09 | `GATE-10` | | An Escape as the first character, in the middle, and as the last character | Each is refused |
| UNIT-CHECKS-10 | `GATE-10` | | A carriage return followed by a line break | Refused |
| UNIT-CHECKS-11 | `GATE-16` | | In turn, a command that holds each mark that changes the reading order | Each is refused with `control_character` |
| UNIT-CHECKS-12 | `GATE-16` | | In turn, a command that holds a zero-width space, a zero-width joiner, a soft hyphen, a byte order mark and an invisible tag character | Each is refused |
| UNIT-CHECKS-13 | `GATE-16` | | A command that holds a line separator, and one that holds a paragraph separator | Each is refused |
| UNIT-CHECKS-14 | `GATE-16` | | A command with an emoji built from joined parts | Refused |

## `line`

The `line` holds the requests that wait for the prompt, oldest first.

### What it holds

- The waiting requests, in the order they came.
- The limit, `MAX_WAITING_REQUESTS`.

A request at the prompt has left the line, but it still waits for the user. So it counts toward the limit: with the limit at 20 and one request at the prompt, 19 more fit. A command that runs does not count.

### What it can be asked

| Ask | Does | Gives back |
|---|---|---|
| `add`, with a request and whether a prompt is on screen | Puts the request at the end, if there is room | Yes, or no when the line is full |
| `next` | Takes out the oldest request | That request, or nothing |
| `remove`, with an `id` | Takes out the request with that `id` | Yes, or no when it is not there |
| `empty` | Takes out every request | All of them, oldest first |

The `gate` asks `next`. The rest is asked by the loop: `add` for a request that passed `checks`, `remove` on a `cancel`, `empty` when the session ends. The loop then sends the `refused` for a request that did not fit, was removed, or was emptied out.

### Rules the code must keep

- **Order is never changed.** A request leaves only from the front, or by its `id`. The others keep their order.
- **A full line changes nothing.** A request that does not fit is not kept anywhere.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-LINE-01 | `GATE-06` | An empty line | `add` A, B, C. Then `next` three times | A, B, C, in that order |
| UNIT-LINE-02 | `GATE-06` | An empty line | `next` | Nothing |
| UNIT-LINE-03 | `GATE-13` | An empty line, limit 3, no prompt on screen | `add` four requests | The first three fit. The fourth does not |
| UNIT-LINE-04 | `GATE-13` | An empty line, limit 3, a prompt on screen | `add` three requests | The first two fit. The third does not |
| UNIT-LINE-05 | `GATE-13` | A full line, limit 3, no prompt on screen | `next`. Then `add` with a prompt on screen. Then `add` with no prompt on screen | The first does not fit: the request at the prompt still counts. The second fits |
| UNIT-LINE-06 | `GATE-13` | A full line | `add` one more. Then `next` until nothing comes | The same requests as before, in the same order. The one that did not fit is not among them |
| UNIT-LINE-07 | `GATE-06`, `GATE-12` | A line with A, B, C | `remove` B. Then `next` twice | Yes. Then A and C |
| UNIT-LINE-08 | `GATE-12` | A line with A, B | `remove` an `id` that is not there | No. `next` still gives A, then B |
| UNIT-LINE-09 | `SES-03` | A line with A, B, C | `empty` | A, B, C in that order. `next` gives nothing |
| UNIT-LINE-10 | `GATE-13` | Limit 1, a prompt on screen | `add` | It does not fit |

## `runner`

The `runner` runs one approved command, sends what it writes, asks "wait or stop" at the time limit, and kills it when told. It is in two halves:

- **The state machine.** It decides. It never touches a real program, so a test can drive it with no waiting.
- **The starter.** It starts a real program and can kill it. It decides nothing.

### What it holds

- Its state.
- The request that runs, and the handle of its program.
- How the command will be said to have ended, once a kill is sent.
- Whether any byte of the output was replaced.
- The number of the timer it waits for.
- Whether the session's time ran out while this command ran.

### What it talks to

| Piece | The `runner` asks it to |
|---|---|
| The starter | Start the command through the shell. Ask the program to stop. Kill it by force |
| The timers | Call back after a time |
| `terminal` | Show the output, and show how the command was killed, if it was |
| `connection` | Send `output`, `still_running` and `ended` |
| `gate` | Take the next request: `done` |

### States

| State | Meaning |
|---|---|
| `Idle` | Nothing runs |
| `Running` | A command runs and its time limit counts |
| `Asked` | The time limit passed. The AI was asked to wait or stop, and the answer time counts |
| `Stopping` | The command was told to stop and has not ended yet |

```mermaid
stateDiagram-v2
  [*] --> Idle
  Idle --> Running: an approval
  Running --> Idle: the command ends
  Running --> Asked: the time limit passes
  Asked --> Running: wait
  Asked --> Idle: the command ends
  Running --> Stopping: stop, Ctrl+C, cancel, or the session ends
  Asked --> Stopping: stop, no answer in time, Ctrl+C, cancel, or the session ends
  Stopping --> Stopping: still there after KILL_WAIT, killed by force
  Stopping --> Idle: the command ends
```

### Events

| Event | Comes from |
|---|---|
| `run` | `gate`: an approval |
| `output` | The starter: the command wrote something |
| `exited` | The starter: the shell ended, with its exit code |
| `timer` | The timers: a time has passed. It carries the number it was started with |
| `wait`, `stop` | `connection`: the AI's answer |
| `ctrl_c` | `terminal`: one Ctrl+C |
| `cancel` | `connection`: the AI hung up on the request with this `id` |
| `time_up` | `session`: the session's time is up |
| `end` | `session`: the session ends |

### The table

An empty "Does" means the event is dropped. "Kill" means: ask the program to stop, note how the command ended, and start a timer of `KILL_WAIT`.

| State | Event | Does | Next |
|---|---|---|---|
| `Idle` | `run`, the approval works | Starts the command through `COMMAND_SHELL`. Starts a timer of `COMMAND_TIME_LIMIT` | `Running` |
| `Idle` | `run`, the approval works, the start fails | Sends `ended` with `start_failed` and the reason as output. Tells `gate` `done` | `Idle` |
| `Idle` | `run`, the approval does not work | | `Idle` |
| `Idle` | any other | | `Idle` |
| `Running` | `run` | | `Running` |
| `Running` | `output` | Shows it. Sends `output` | `Running` |
| `Running` | `exited` | Sends `ended` with `exit` and the exit code. Tells `gate` `done` | `Idle` |
| `Running` | `timer`, the one it waits for | Sends `still_running`. Starts a timer of `QUESTION_WAIT` | `Asked` |
| `Running` | `timer`, the one it waits for, after `time_up` | Kill, `killed_time_up`. No `still_running` is sent | `Stopping` |
| `Running` | `time_up` | Notes it. Nothing is killed | `Running` |
| `Running` | `wait` | | `Running` |
| `Running` | `stop` | Kill, `killed_by_ai` | `Stopping` |
| `Running` | `ctrl_c` | Kill, `killed_by_user` | `Stopping` |
| `Running` | `cancel`, its own `id` | Kill, `killed_ai_gone` | `Stopping` |
| `Running` | `cancel`, another `id` | | `Running` |
| `Running` | `end` | Kill, `killed_session_ended` | `Stopping` |
| `Asked` | `run` | | `Asked` |
| `Asked` | `output` | Shows it. Sends `output` | `Asked` |
| `Asked` | `exited` | Sends `ended` with `exit` and the exit code. Tells `gate` `done` | `Idle` |
| `Asked` | `wait` | Starts a timer of `COMMAND_TIME_LIMIT` | `Running` |
| `Asked` | `wait`, after `time_up` | Kill, `killed_time_up` | `Stopping` |
| `Asked` | `time_up` | Notes it. Nothing is killed | `Asked` |
| `Asked` | `stop` | Kill, `killed_by_ai` | `Stopping` |
| `Asked` | `timer`, the one it waits for | Kill, `killed_no_answer` | `Stopping` |
| `Asked` | `ctrl_c` | Kill, `killed_by_user` | `Stopping` |
| `Asked` | `cancel`, its own `id` | Kill, `killed_ai_gone` | `Stopping` |
| `Asked` | `cancel`, another `id` | | `Asked` |
| `Asked` | `end` | Kill, `killed_session_ended` | `Stopping` |
| `Stopping` | `output` | Shows it. Sends `output` | `Stopping` |
| `Stopping` | `exited` | Kills by force what is left of the command. Sends `ended` with the noted way. Tells `gate` `done` | `Idle` |
| `Stopping` | `timer`, the one it waits for | Kills the program by force | `Stopping` |
| `Stopping` | any other | | `Stopping` |
| any | `timer`, another number | | the same |

### Rules the code must keep

- **Nothing runs without an approval that works.** The `runner` takes the request out of the approval, and from nowhere else. A used approval gives nothing. (`GATE-01`, `GATE-15`)
- **One command at a time.** A `run` outside `Idle` is dropped. (`GATE-06`)
- **Every command that starts gets one `ended`, and the `gate` one `done`.** Never two, never none.
- **The first reason to kill wins.** Once a kill is sent, a later `stop` or Ctrl+C does not change how the command is said to have ended.
- **When the session's time is up, a command gets no more time, and is not killed for it at once.** `time_up` only leaves a note on the command that runs. At its next time limit it is killed with no question to the AI, and a `wait` that comes after the note kills it too. The note goes away with the command, so the next command starts clean. (`LEN-08`)
- **A timer that is no longer waited for does nothing.** Each timer carries a number. The `runner` raises the number each time it starts one, and drops a `timer` event with an old number. No timer is ever cancelled.
- **The AI gets text.** Output that is not valid UTF-8 has each bad stretch replaced by the mark `�`, and `ended` then says `bytes_replaced`. A character that arrives split over two pieces is put together first and is not replaced. Nothing else in the output is changed: cleaning is for the screen only, and the `terminal` does it. (`SHOW-03`)

### The starter

- **Through the shell.** It runs `COMMAND_SHELL` with `-c` and the command as one argument, as the user who started the cli-tool. (`GATE-08`, `GATE-11`)
- **No keyboard.** The command's input is empty. (`GATE-17`)
- **Plain surroundings.** `PAGER=cat` and `TERM=dumb` are added to what the cli-tool itself was started with. (`GATE-18`)
- **Both outputs, one stream.** Normal and error output go into the same pipe, so their order is kept.
- **A group of its own.** The command starts in a new process group. A kill goes to the whole group, so every program the command started gets it. (`LIM-11`)
- **Asking, then force.** "Ask to stop" is the signal `TERM`. "Kill by force" is the signal `KILL`. (`LIM-13`)
- **Over when the shell ends.** The exit code is the shell's. A shell ended by a signal gives 128 plus the signal's number, as shells do. A program left in the background is not killed and is not waited for. Half a second after the shell ends, the starter stops reading the output, so such a program must write to a file. (`LIM-12`)

### Unit tests

Tests 01 to 20 and 32 to 35 run the state machine with a fake starter, fake timers and a fake `terminal`, `connection` and `gate`. Tests 21 to 31 run the real starter with real commands in a scratch folder.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-RUNNER-01 | `GATE-01`, `GATE-11` | `Idle` | `run` with a fresh approval | `Running`. The starter got `COMMAND_SHELL` and the exact command. A timer of `COMMAND_TIME_LIMIT` was started |
| UNIT-RUNNER-02 | `GATE-15` | `Idle` | `run` with a used approval, and with no approval | `Idle`. Nothing was started or sent |
| UNIT-RUNNER-03 | `GATE-06` | `Running` | `run` with a second fresh approval | Nothing more was started. The second approval is still unused |
| UNIT-RUNNER-04 | `SHOW-02` | `Running` | `output` twice | The screen and the connection each got both pieces, in order |
| UNIT-RUNNER-05 | `LIM-01` | `Running` | `exited` with code 3 | `Idle`. `ended` with `exit` and 3. `gate` got `done` once. No `still_running` |
| UNIT-RUNNER-06 | `LIM-01` | `Running` | The `timer` | `Asked`. `still_running` was sent. A timer of `QUESTION_WAIT` was started. Nothing was killed |
| UNIT-RUNNER-07 | `LIM-02` | `Asked` | `wait`, then the new `timer` | `Running` after `wait`, with a new timer of `COMMAND_TIME_LIMIT`. Then `Asked` and a second `still_running` |
| UNIT-RUNNER-08 | `LIM-02` | `Asked` | `stop`, then `exited` | The program was asked to stop. Then `ended` with `killed_by_ai`, and `Idle` |
| UNIT-RUNNER-09 | `LIM-03` | `Asked` | The `timer`, then `exited` | The program was asked to stop. Then `ended` with `killed_no_answer` |
| UNIT-RUNNER-10 | `LIM-04` | `Asked` | `exited` with code 0 | `Idle`. `ended` with `exit` and 0. Nothing was killed |
| UNIT-RUNNER-11 | `LIM-08` | `Running` | `ctrl_c`, then `exited` | The program was asked to stop. `ended` with `killed_by_user`. `Idle`, and `gate` got `done` |
| UNIT-RUNNER-12 | `LIM-07` | `Running` | `cancel` for another `id`, then `cancel` for its own, then `exited` | The first does nothing. The second asks the program to stop. `ended` with `killed_ai_gone` |
| UNIT-RUNNER-13 | `SES-05` | `Running`, and again `Asked` | `end`, then `exited` | The program was asked to stop. `ended` with `killed_session_ended` |
| UNIT-RUNNER-14 | `LIM-02`, `LIM-03` | `Asked` | `wait`, then the old `timer` of the question. Then `exited`, then every old `timer` | The old timer kills nothing. After the end, no timer does anything |
| UNIT-RUNNER-15 | `LIM-13` | `Stopping` after `ctrl_c` | The `timer` of `KILL_WAIT` | The program was killed by force. Still `Stopping` until `exited` |
| UNIT-RUNNER-16 | `LIM-08` | `Stopping` after `ctrl_c` | `stop`, `cancel`, `end`, `ctrl_c`, then `exited` | The program was asked to stop once only. `ended` with `killed_by_user` |
| UNIT-RUNNER-17 | `LIM-02` | `Stopping` | `output`, then `exited` | The output was shown and sent before `ended` |
| UNIT-RUNNER-18 | `SHOW-03` | `Running` | `output` with a bad byte. `output` with a Greek letter split over two pieces. `output` that ends in half a letter, then `exited` | The bad byte and the half letter each came as the mark. The split letter came whole. `ended` says `bytes_replaced`. A run with clean output does not say it |
| UNIT-RUNNER-19 | `GATE-11` | `Idle`, a starter that fails | `run` with a fresh approval | `Idle`. `ended` with `start_failed`, and the reason was sent as output. `gate` got `done` |
| UNIT-RUNNER-20 | `GATE-01`, `LIM-02` | `Idle`, and `Running` | In `Idle`: every event but `run`. In `Running`: `stop` | In `Idle` nothing was started, killed or sent. In `Running`, `stop` asks the program to stop, and `ended` says `killed_by_ai` |
| UNIT-RUNNER-21 | `GATE-11` | The real starter | Two commands joined by a pipe | The output of the second, fed by the first. Exit code 0 |
| UNIT-RUNNER-22 | `GATE-11` | The real starter | A command that writes to a file with `>`, then `;`, then a second command | The file holds the text. The second command ran too |
| UNIT-RUNNER-23 | `GATE-08` | The real starter | A command that prints the user's number | The number of the user who runs the test |
| UNIT-RUNNER-24 | `GATE-01` | The real starter | `exit 7` | Exit code 7 |
| UNIT-RUNNER-25 | `SHOW-02` | The real starter | A command that writes to normal output, then error output, then normal output | All three, in that order |
| UNIT-RUNNER-26 | `GATE-17` | The real starter | `cat` with no file | It ends at once with no output and exit code 0 |
| UNIT-RUNNER-27 | `GATE-18` | The real starter | A command that prints `PAGER` and `TERM` | `cat` and `dumb` |
| UNIT-RUNNER-28 | `LIM-11` | The real starter, a command that starts a long program in the background, prints its number and waits | Ask it to stop | The shell ends, and the background program is gone |
| UNIT-RUNNER-29 | `LIM-13` | The real starter, a command that does not stop when asked | Ask it to stop. Then kill by force | It still runs after the asking. It is gone after the force |
| UNIT-RUNNER-30 | `LIM-12` | The real starter | A command that starts a long program in the background with its output to a file, prints its number, and ends. Then the same with the output not sent to a file | Both times the exit comes in under two seconds and the program still runs. The test then kills it |
| UNIT-RUNNER-31 | `GATE-11` | The real starter, a shell that is not there | Start a command | An error. No exit is reported |
| UNIT-RUNNER-32 | `LEN-08` | `Running` | `time_up`. Then the `timer`, then `exited` | Still `Running` after `time_up`, and nothing was killed. At the timer the program was asked to stop and no `still_running` was sent. `ended` with `killed_time_up` |
| UNIT-RUNNER-33 | `LEN-08` | `Asked`, twice | `time_up`, then `wait`, then `exited`. The second time: `time_up`, then the `timer` of the question, then `exited` | The `wait` gives no new time limit: the program was asked to stop, and `ended` says `killed_time_up`. With no answer, `ended` says `killed_no_answer` |
| UNIT-RUNNER-34 | `LEN-08` | `Running` | `time_up`, then `exited` with code 0. Then a new approval, and its `timer` | `ended` with `exit` and 0. The next command gets `still_running` at its time limit, as usual |
| UNIT-RUNNER-35 | `LEN-08` | `Idle` | `time_up`. Then an approval, and its `timer` | Nothing was sent or killed. The command gets `still_running` at its time limit, as usual |

## `terminal`

The `terminal` is the only piece that reads keys and writes to the screen. It is in three parts:

- **The keys.** Cuts the bytes from the keyboard into key presses, and tells one Ctrl+C from two.
- **The screen.** Writes the prompt, the answers, the output and the notes. Cleans all of it.
- **The raw mode.** Sets the real terminal so that keys come at once, and puts it back at the end.

### What it holds

- An Escape or a key of many bytes that is not over yet.
- The time of the last Ctrl+C that a second one can pair with.
- Whether the cursor is at the start of a line.
- The request whose prompt waits on screen, if one does.
- The question to extend that waits on screen, if one does.

### What it sends

| Event | Meaning |
|---|---|
| `key` | One key press that is not Ctrl+C, as the bytes the key sent |
| `ctrl_c` | One Ctrl+C |
| `quit` | The second of two Ctrl+C within `DOUBLE_CTRL_C_WINDOW` |

The loop passes `key` to the `gate`. It passes `ctrl_c` to whatever is in front, and `quit` ends the cli-tool.

### The keys

| Bytes | Become |
|---|---|
| Code 3 | `ctrl_c`, or `quit` |
| Escape, `[` or `O`, and all that follows up to a byte from `@` to `~` | One `key` of all those bytes: an arrow, a function key |
| Escape and one other byte | One `key` of two bytes |
| A letter of more than one byte | One `key` of those bytes |
| Any other byte | One `key` of one byte |

- **A doubt drops a key and never makes one up.** Only a `key` of the single byte `y` or `n` can answer a prompt. So when the cutting is not sure, it joins bytes into one key, which answers nothing. (`GATE-03`)
- **A key can come in two reads.** An Escape at the end of one read is joined with the start of the next.
- **Ctrl+C is never eaten.** A code 3 inside an unfinished key ends that key and still counts. (`LIM-10`)
- **Two Ctrl+C.** A Ctrl+C is `quit` when the one before it was a `ctrl_c` and came no more than `DOUBLE_CTRL_C_WINDOW` earlier. Keys pressed between the two change nothing. After a `quit`, the next Ctrl+C is a `ctrl_c` again.

### The screen

The wording is in `ux.md`.

- **Everything is cleaned.** Every character that `checks` would refuse in a request is replaced by the mark `�` before it is written: in output, and also in the command of a prompt and in notes. Line breaks and tabs stay. (`SHOW-03`, `SHOW-05`)
- **Output is marked.** Every line of a command's output starts with `  | `. No line of the cli-tool itself starts with a space. So output that copies the words of a prompt still does not look like one. (`SHOW-04`)
- **The cli-tool's lines start at the start of a line.** If output ended with no line break, a line break is written first.
- **The prompt shows the request as it is.** Each line of the command is shown on a line of its own, after `  > `. (`GATE-05`)
- **The prompt is the last thing on screen.** A note that comes while a prompt waits is written, and the prompt is then shown again under it. The same holds for the question to extend.
- **A time is written in words.** The question to extend names the length as `30 minutes`, `1 hour` or `90 seconds`. (`LEN-03`)
- **The maximum is named.** When the stretch a `y` would give ends at the maximum, the question says so, and the line after the `y` says that the session then ends. Such a stretch is rounded up to whole seconds. (`LEN-05`)
- **A refusal with no prompt shows the reason only.** The screen is never handed the refused text. (`GATE-10`, `GATE-16`)

Known limit: a line of output longer than the window is folded by the terminal, and the folded part has no mark. The real prompt still comes last, under it.

### The raw mode

- Keys come one by one with no Enter, and are not shown.
- Ctrl+C comes as a key, not as a signal. So does Ctrl+Z.
- Output is left as it was, so a line break still starts a new line.
- The settings found at the start are kept and put back at the end. (`HOST-07`)

### Unit tests

Tests 01 to 10 are for the keys, 11 to 23 and 28 to 31 for the screen, with a buffer in place of the screen and a clock the test moves. Tests 24 to 27 use a real terminal that the test makes for itself.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-TERMINAL-01 | `GATE-03` | | The bytes `yes` in one read | Three `key`: `y`, `e`, `s` |
| UNIT-TERMINAL-02 | `GATE-03` | | An arrow key, a function key and a key that starts with Escape `O`, in one read | Three `key`, each with all its bytes |
| UNIT-TERMINAL-03 | `GATE-03` | | A Greek letter and an emoji | Two `key`, each with all its bytes |
| UNIT-TERMINAL-04 | `GATE-03` | | An arrow key cut over two reads, at each place it can be cut | One `key` each time, and no other |
| UNIT-TERMINAL-05 | `GATE-03` | | Escape, then `y`, in two reads | One `key` of two bytes. No `key` is the single byte `y` |
| UNIT-TERMINAL-06 | `LIM-09` | | Code 3 | One `ctrl_c`. No `key` |
| UNIT-TERMINAL-07 | `LIM-10` | | Escape, `[`, then code 3 | One `key` for the unfinished bytes, then one `ctrl_c` |
| UNIT-TERMINAL-08 | `LIM-10` | | Two code 3: in one read, and in two reads within `DOUBLE_CTRL_C_WINDOW` | `ctrl_c`, then `quit`, both times |
| UNIT-TERMINAL-09 | `LIM-10` | | Two code 3 further apart than `DOUBLE_CTRL_C_WINDOW`. Then two exactly that far apart | Two `ctrl_c` and no `quit`. Then `ctrl_c` and `quit` |
| UNIT-TERMINAL-10 | `LIM-10` | | Code 3, then `x`, then code 3, within the window. Then a fourth code 3 at once | `ctrl_c`, `key`, `quit`. Then `ctrl_c`, not a second `quit` |
| UNIT-TERMINAL-11 | `SHOW-01`, `GATE-05` | | A prompt for a command | The screen shows the exact command after `  > `, then the question. The cursor stays on the question's line |
| UNIT-TERMINAL-12 | `GATE-05` | | A prompt for a command of three lines with a tab | Three lines, each after `  > `, with the tab as it is |
| UNIT-TERMINAL-13 | `SHOW-01` | A prompt on screen | The answer: yes, no, and "the AI left" | `y`, `n`, and a line that says the AI hung up and nothing ran. Each ends its line |
| UNIT-TERMINAL-14 | `SHOW-02` | | Output of two lines, then the end with exit code 0. Again with exit code 3 | Both lines, and no line about the end |
| UNIT-TERMINAL-15 | `SHOW-02` | | The end, for each way a command can end | Nothing for a command that ended by itself. A line for each other way. No two are the same |
| UNIT-TERMINAL-16 | `SHOW-03` | | Output with an Escape, a colour code, a carriage return, a bell, a backspace, a zero byte, code 127 and code 155 | None of them is written. A mark stands in each place. Line breaks and tabs are written |
| UNIT-TERMINAL-17 | `SHOW-05` | | Output with a character that turns the reading order around, and a zero-width space | Neither is written. A mark stands in each place |
| UNIT-TERMINAL-18 | `SHOW-04` | | Output of many lines in one piece. A line that comes in two pieces. Output that copies a prompt | Every line starts with `  | `, once. No line starts with the words of a prompt |
| UNIT-TERMINAL-19 | `SHOW-04` | Output that ended with no line break | The end of the command. Again with a new prompt | What the cli-tool writes starts on a new line |
| UNIT-TERMINAL-20 | `SHOW-01` | | A refusal with no prompt, for a control character and for a full line | A line each, that says a request was refused and why |
| UNIT-TERMINAL-21 | `SHOW-01` | A prompt on screen | A refusal with no prompt | The note, then the same prompt again as the last thing on screen |
| UNIT-TERMINAL-22 | `KEY-04` | | The paste line for an address and a key | A line that is exactly `/gangway`, the address and the key, with one space between |
| UNIT-TERMINAL-23 | `SHOW-03` | | A prompt, a note and a paste line that hold an Escape | No Escape is written |
| UNIT-TERMINAL-24 | `GATE-03` | A real terminal in raw mode | `y` is typed with no Enter | The `y` can be read at once |
| UNIT-TERMINAL-25 | `LIM-09` | A real terminal in raw mode | Ctrl+C is typed | Code 3 can be read |
| UNIT-TERMINAL-26 | `HOST-02` | | Ask whether a real terminal is a terminal. Ask the same of a plain file | Yes. No |
| UNIT-TERMINAL-27 | `HOST-07` | A real terminal in raw mode | Put it back | Its settings are the ones found at the start |
| UNIT-TERMINAL-28 | `LEN-03` | | The question to extend, for 30 minutes. Then the answer: yes, no, and no answer in time | A line that says the time is up, then the question with the length, and the cursor stays on its line. `y` and a line that says how long the session goes on, `n`, and a line that says no answer came. Each ends its line |
| UNIT-TERMINAL-29 | `LEN-03`, `LEN-06` | The question to extend on screen | A refusal with no prompt, for the time being up. Then the question is forgotten, and a note comes | The note says a request was refused because the time is up, and the same question is again the last thing on screen. After it is forgotten, a note does not bring it back |
| UNIT-TERMINAL-30 | `LEN-03` | | Write in words: 30 minutes, 1 minute, 2 hours, 1 hour, 90 seconds, 1 second, and a time and a half second | `30 minutes`, `1 minute`, `2 hours`, `1 hour`, `90 seconds`, `1 second`, and for the last one a form with its unit |
| UNIT-TERMINAL-31 | `LEN-07`, `LEN-08` | A prompt on screen | The answer "the time is up". Then the end of a command that was killed because the time was up | A line that says the time is up and nothing ran. A line that says the command was killed and why, unlike the line of every other way to end |
| UNIT-TERMINAL-32 | `LEN-05` | | The question to extend, for 5 minutes, up to the maximum. Then the answer yes with a stretch a little shorter. Again: a question for a second and a half, closed because the maximum came. Then a note | The question says "up to the maximum". After `y`, a line with the stretch in whole seconds, rounded up, that says the maximum is reached then. The second question names 2 seconds, is closed with a line break only, and the note does not bring it back |

## `session`

The `session` makes the session key and the paste line, and runs the session's clock: when the time is up, the question to extend, the maximum, and the end.

### What it holds

- The API address, as the AI must call it. The controller gives it in `session_ok`. It is taken only when it is a plain web address: it starts with `http://` or `https://`, names a host, and holds letters, digits and the usual marks of an address only. (`KEY-10`)
- The session key.
- Its state.
- The length the session started with: `SESSION_LENGTH`, or the length asked for at start.
- The end time, and the number of the timer it waits for.
- The maximum's time: the moment the clock started plus `SESSION_MAX`. With no `SESSION_MAX` there is none. It is set once, when the clock starts.
- Whether the stretch that counts ends at the maximum.
- The time the question to extend was shown.

### What it talks to

| Piece | The `session` asks it to |
|---|---|
| The clock | Tell the time |
| The timers | Call back after a time. They are the session's own, apart from the ones of the `runner` |
| `terminal` | Show the question to extend, and show the answer |
| `connection` | Send `time_up` and `extended` |
| The loop | Refuse what waits, when the time is up. End the session, when it is not extended, and when the maximum is reached |

### States

| State | Meaning |
|---|---|
| `Live` | The session takes requests. The clock counts to the end time |
| `TimeUp` | The time is up. New requests are refused. A command still runs, and the question waits for it to end |
| `Extending` | The question to extend is on the screen, and `EXTEND_WAIT` counts |
| `Ended` | The session is over. Final |

```mermaid
stateDiagram-v2
  [*] --> Live: the key is made
  Live --> TimeUp: the time is up
  TimeUp --> Extending: no command runs, below the maximum
  TimeUp --> Ended: no command runs, at the maximum
  Extending --> Live: y
  Extending --> Ended: n, Ctrl+C, no answer in time, or the maximum comes
  Live --> Ended: end
  TimeUp --> Ended: end
  Ended --> Ended: end
  Ended --> [*]
```

There is no event that leads from `Ended` to another state. A new start of the cli-tool makes a new session with a new key.

### Events

| Event | Comes from |
|---|---|
| `timer` | The timers: a time has passed. It carries the number it was started with |
| `request` | The loop: a request came, and asks whether it may go on |
| `idle` | The loop: no command runs. Sent after the time is up, and again each time a command ends |
| `key` | `terminal`: one key press, passed on by the loop while the question waits |
| `ctrl_c` | `terminal`: one Ctrl+C, passed on by the loop while the question waits |
| `end` | The loop: the session ends, for any reason |

### The table

An empty "Does" means the event is dropped. "Time up" means: send `time_up`, and tell the loop to refuse what waits. "The next stretch" is the length the session started with, or what is left to the maximum when that is less.

| State | Event | Does | Next |
|---|---|---|---|
| `Live` | `timer`, the one it waits for | Time up | `TimeUp` |
| `Live` | `request`, before the end time | Says yes | `Live` |
| `Live` | `request`, at or after the end time | Time up. Says no | `TimeUp` |
| `Live` | `idle`, `key`, `ctrl_c` | | `Live` |
| `TimeUp` | `request` | Says no | `TimeUp` |
| `TimeUp` | `idle`, the stretch that ran out ended at the maximum, or nothing is left to it | Tells the loop that the maximum is reached | `Ended` |
| `TimeUp` | `idle`, below the maximum | Shows the question, with the next stretch. Notes the time. Starts a timer of `EXTEND_WAIT`, or of what is left to the maximum when that is less | `Extending` |
| `TimeUp` | `timer`, `key`, `ctrl_c` | | `TimeUp` |
| `Extending` | `request` | Says no | `Extending` |
| `Extending` | any `key`, less than `PROMPT_MIN_SHOW` after the question was shown | | `Extending` |
| `Extending` | `key` `y`, below the maximum | Sets the end time to now plus the next stretch. Starts a timer of that stretch. Shows the answer, and sends `extended`, both with that stretch | `Live` |
| `Extending` | `key` `y`, nothing is left to the maximum | Closes the question. Tells the loop that the maximum is reached | `Ended` |
| `Extending` | `key` `n` | Shows the answer. Tells the loop to end the session | `Ended` |
| `Extending` | any other `key` | | `Extending` |
| `Extending` | `ctrl_c` | Shows the answer, as for `n`. Tells the loop to end the session | `Ended` |
| `Extending` | `timer`, the one it waits for, of `EXTEND_WAIT` | Shows that no answer came. Tells the loop to end the session | `Ended` |
| `Extending` | `timer`, the one it waits for, of what was left to the maximum | Closes the question. Tells the loop that the maximum is reached | `Ended` |
| `Extending` | `idle` | | `Extending` |
| any | `timer`, another number | | the same |
| any | `end` | | `Ended` |
| `Ended` | any | | `Ended` |

### Rules the code must keep

- **The key is random.** It is `gw_` and then `SESSION_KEY_BITS` bits from the system's random source, written in lower-case hex. (`KEY-01`)
- **No random bytes, no session.** When the random source fails or gives too few bytes, no session is made. Nothing weaker takes its place. (`KEY-01`)
- **A weak length is refused.** `SESSION_KEY_BITS` must be a multiple of 8 and at least 128. A mistake in the settings must not make a key that can be guessed. (`KEY-01`)
- **The paste line is `/gangway`, the address, the key, with one space between.** An address that is empty, or holds a space or a control character, is refused, because the line would not split into its three parts. (`KEY-04`)
- **The controller gets the hash, never the key.** The `session` gives the hash for `session_start`. The key itself goes to the user's screen in the paste line and nowhere else. (`SES-06`)
- **Nothing goes to disk.** The code of the `session` uses no package that can write a file. (`SES-06`)
- **Ended is final.** (`SES-03`)
- **One place gives more time.** The end time moves in a single place: `Extending` with `key` `y`. No other event can move it, and no message from the controller or the AI is an event of the `session`. (`LEN-09`)
- **Exactly two keys answer, and a fresh question takes no key.** The rules are the ones of the `gate`: the single byte `y` or `n`, and nothing in the first `PROMPT_MIN_SHOW`. A `y` meant for a command's prompt, pressed just as the time runs out, then does not extend the session. `ctrl_c` is not held back: it can only end. (`LEN-03`, `LEN-04`)
- **The same length again.** A `y` gives one more stretch of the length the session started with, counted from the moment of the `y`. (`LEN-03`)
- **No answer is a no.** When `EXTEND_WAIT` passes with no answer, the session ends. (`LEN-04`)
- **The question waits for a running command.** A command that runs when the time is up is left alone, and the question is shown when it is over. (`LEN-08`)
- **The maximum caps the whole session.** A stretch that would pass the maximum ends at the maximum, the first one too. When such a stretch runs out there is no question: the session ends. A command that runs then is waited for, as at every time up. (`LEN-05`)
- **The maximum never moves.** It is set in one place, when the clock starts, from the value `settings` read in the admin file. No key, no message and no later event changes it. (`LEN-05`, `LEN-09`)
- **The question does not wait past the maximum.** When less than `EXTEND_WAIT` is left to the maximum, the question waits only that long, and then the session ends. (`LEN-05`)
- **A late timer lets nothing in.** The end time is also checked each time a request comes, by the time of day. A timer that comes late, as on a machine that slept, cannot let a request through after the end time. (`LEN-06`)
- **A timer that is no longer waited for does nothing.** Each timer carries a number, as in the `runner`. The timer of a stretch that was ended early, and the timer of a question that was answered, are dropped.

### Unit tests

Tests 10 to 25 are for the clock: a session with a clock the test moves, fake timers, and a fake `terminal`, `connection` and loop that record what they were asked.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-SESSION-01 | `KEY-01` | | Make a session with 128, 256 and 512 bits | Each key is `gw_` and then 32, 64 and 128 lower-case hex characters |
| UNIT-SESSION-02 | `KEY-01`, `SES-03` | | Make a hundred sessions | All keys differ |
| UNIT-SESSION-03 | `KEY-01` | A random source that fails after 0, 5 and 31 bytes | Make a session of 256 bits | An error each time. No session |
| UNIT-SESSION-04 | `KEY-01` | | Make a session with a length below 128 bits, and with one that is not a multiple of 8 | An error each time. No session |
| UNIT-SESSION-05 | `KEY-04` | A session | Read the paste line | `/gangway`, the address and the key, with one space between |
| UNIT-SESSION-06 | `KEY-04`, `KEY-10` | A session | Give it an address that is empty, or holds a space, a tab, a line break, an Escape or an invisible character, or does not start with `http://` or `https://` | An error each time, and the error does not hold the address. The session has no address |
| UNIT-SESSION-07 | `SES-03` | A live session | End it. End it again. Read the key, the hash and the paste line | Not live after the first end, and not live after anything that follows |
| UNIT-SESSION-08 | `SES-06` | A session | Read the hash | It is the hash the contract sets for that key. It does not hold the key |
| UNIT-SESSION-09 | `SES-06` | The code of the `session` | List the packages it uses | Each is on a short list of packages that cannot write a file |
| UNIT-SESSION-10 | `LEN-01`, `LEN-02` | A session whose clock starts with a length of 30 minutes. Again with 10 minutes | Look at the timers. A `request` just before the length has passed | One timer of that length was started. The request may go on, and nothing was sent or shown |
| UNIT-SESSION-11 | `LEN-01`, `LEN-06` | `Live` | The `timer`. Then `request` | `TimeUp`. `time_up` was sent once, and the loop was told once to refuse what waits. The request may not go on. No question yet |
| UNIT-SESSION-12 | `LEN-03`, `LEN-08` | `TimeUp` | `idle`, twice | `Extending`. The question was shown once, with the length the session started with. A timer of `EXTEND_WAIT` was started |
| UNIT-SESSION-13 | `LEN-03` | `Extending`, `PROMPT_MIN_SHOW` has passed | `key` `y`. Then `request` | `Live`. The answer was shown. `extended` was sent with the length in seconds. A new timer of the length was started. The request may go on |
| UNIT-SESSION-14 | `LEN-03` | A session of 10 minutes that was extended once | The clock moves by just under 10 minutes: `request`. Then the old timers, then the new one | The request may go on. The old timers do nothing. The new one: `TimeUp`, and `time_up` was sent a second time |
| UNIT-SESSION-15 | `LEN-04` | `Extending`, three times | `key` `n`. `ctrl_c`, with no time passed. The `timer` of `EXTEND_WAIT` | Each time the loop was told once to end the session, and nothing was extended. The screen got "no" twice, then "no answer" |
| UNIT-SESSION-16 | `LEN-03` | `Extending`, the question just shown | `key` `y` and `key` `n` with no time passed. Then, after `PROMPT_MIN_SHOW`: `Y`, `N`, Enter, space, Escape, an arrow key | Still `Extending` after each one. Nothing was shown or sent, and the loop was told nothing |
| UNIT-SESSION-17 | `LEN-09` | `Live`, and again `TimeUp` | `key` `y`, `ctrl_c` and `idle` in `Live`. `key` `y` and `ctrl_c` in `TimeUp` | The state and the end time have not moved. No timer was started, and `extended` was not sent |
| UNIT-SESSION-18 | `LEN-06` | `Live`, the clock moved past the end time, and no timer has come | `request` | The request may not go on. `TimeUp`, `time_up` was sent, and the loop was told to refuse what waits. The late `timer` then does nothing more |
| UNIT-SESSION-19 | `SES-03`, `LEN-04` | `Live`, `TimeUp` and `Extending`, in turn | `end`. Then every event | `Ended`. Nothing more was shown, sent or started, and a request may not go on |
| UNIT-SESSION-20 | `LEN-09` | The code of the `session` | Search it for the places that set the end time | There is one, and it is reached from two places only: the start of the clock, and `key` `y` in `Extending` |
| UNIT-SESSION-21 | `LEN-05` | A session of 10 minutes with a maximum of 25 minutes | Let each stretch run out and press `y`, twice. Then let the last stretch run out | The second question names what is left to the maximum, and says so. The third stretch is that long, `extended` carries it, and the end time is the maximum. Then the loop was told that the maximum is reached. No third question |
| UNIT-SESSION-22 | `LEN-05`, `SET-01` | A maximum of 20 minutes. A session with a length of 20 minutes, of 30 minutes and of one hour | A `request` just before the maximum. The `timer`, then `idle` | One timer of 20 minutes was started. The request may go on. Then `time_up` was sent, the loop was told that the maximum is reached, and no question was shown |
| UNIT-SESSION-23 | `LEN-05`, `LEN-08` | A session whose length is its maximum. A command runs | The `timer`. `key` `y` and `ctrl_c`. Then `idle` | `TimeUp`, and the loop was not told that the maximum is reached. The keys do nothing. After `idle` it was told, and no question was shown |
| UNIT-SESSION-24 | `LEN-05` | The question is shown with 30 seconds left to the maximum. Again with more left than `EXTEND_WAIT` | The `timer` of the question. Again: `key` `y` when the maximum has come and the timer has not. Again: the `timer` | The question waits 30 seconds, then the loop was told that the maximum is reached, not that no answer came. The `y` extends nothing and the same was told. With more left, the question waits `EXTEND_WAIT` and no answer is a no |
| UNIT-SESSION-25 | `LEN-05`, `LEN-09`, `SET-03` | The code of the `session`. A session with a maximum, extended twice. A session with no maximum, extended fifty times | Search the code for the places that set the maximum. Compare the maximum before and after | It is set in one place, at the start of the clock, and it has not moved. With no maximum every stretch is the full length and no maximum is named |

## `connection`

The `connection` holds the websocket to the controller. It turns the cli-tool's answers into messages, and the controller's messages into events for the loop. It decides nothing about a request.

There is no reconnect in the MVP. A lost connection is told once, and that is the end. Reconnect adds states here when it is built.

It knows no address and no road of its own: `network` gives it what opens a connection, and the controller's address on that road. It hands the API address of `session_ok` back as it came; `session` checks it.

### What it holds

- The websocket.
- The messages that wait to be sent, in order, and their size.
- Whether it is closed, and whether the end was asked for.

### What it talks to

| Piece | How |
|---|---|
| The controller | Messages of `architecture.md`, one JSON object each, over the websocket at the path `/host/v1` |
| The loop | The `connection` calls a handler, one call for each message that counts. The handler hands the call over to the loop and comes back at once |
| `gate`, `runner`, the loop | They call the `connection` to send: `started`, `refused`, `output`, `still_running`, `ended`, and `session_end` at the close |
| `session` | It calls the `connection` to send `time_up` and `extended` |

### States

| State | Meaning |
|---|---|
| `Opening` | The websocket is being opened. `session_start` is sent and `session_ok` is waited for |
| `Live` | Messages go both ways |
| `Closed` | Nothing is sent and nothing is handed over. Final |

```mermaid
stateDiagram-v2
  [*] --> Opening: dial
  Opening --> Live: session_ok
  Opening --> Closed: anything else, or no answer in time
  Live --> Closed: close, asked by the loop
  Live --> Closed: session_end from the controller
  Live --> Closed: the connection is lost
  Closed --> [*]
```

### What comes in

| Message from the controller | Event for the loop |
|---|---|
| `request`, with an `id` and the kind `command` | A request, with that `id` and the command as it came |
| `request`, with an `id` and another kind | None. The `connection` sends `refused` with `unknown_kind` |
| `wait`, `stop`, `cancel`, with an `id` | The same, with that `id` |
| `session_end` | The controller ends the session, with its reason. The last event |
| Any of the above with no `id`, where one is needed | None |
| Any other type, text that is not JSON, or a message that is not text | None |

When the connection goes away and neither side asked for it, the loop gets one event: the connection is lost. It is the last event.

### Rules the code must keep

- **No session without `session_ok`.** Opening fails unless the controller's first message is `session_ok`. It also fails when no answer comes in ten seconds. (`SES-01`)
- **The key never goes on the connection.** The `connection` is handed the hash. It never sees the key. (`SES-06`)
- **Only the listed messages do anything.** There is no code for a message that approves a request, extends a session or changes a setting. A field the contract does not name, such as "approved", is not read. (`GATE-07`)
- **A request comes through as it was sent.** The command is not trimmed, cleaned or changed. The checks come after, in `checks`. (`GATE-05`)
- **Sending never waits for the network.** A send puts the message in a line and comes back. One goroutine writes the line to the websocket, in order. The loop is then never held up by a slow controller, and Ctrl+C always works. (`LIM-10`)
- **A controller that does not take what is sent is a lost connection.** When more than 8 MB wait to be sent, the connection is cut and told as lost. Memory does not fill up.
- **One last event.** After "lost" or "the controller ends the session", no event follows. A close asked by the loop gives no "lost".
- **A close sends `session_end` last.** What waited to be sent goes out first, then `session_end`, then the websocket is closed. The close waits two seconds at most. (`SES-02`)
- **After the end, sends are dropped.** They do not wait and do not fail.
- **A message from the controller may be 1 MB at most.** A larger one cuts the connection.

### Unit tests

Each test runs the real `connection` against a controller inside the test: a small web server that speaks websocket and does what the test tells it.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-CONNECTION-01 | `SES-06` | A controller that answers `session_ok` | Dial with a hash, a host name, limits and a length | The controller's first message is `session_start` at the path `/host/v1`, with that hash, name, limits and length, and with no key in it. The dial comes back with no error |
| UNIT-CONNECTION-02 | `SES-01` | A controller whose first message is, in turn: a `request`, text that is not JSON, a message that is not text | Dial | An error each time. The loop gets no event |
| UNIT-CONNECTION-03 | `SES-01` | In turn: a controller that closes with no answer, a controller that never answers, no controller at all | Dial | An error each time, the second one when the time limit of the dial passes |
| UNIT-CONNECTION-04 | `GATE-05` | Live | The controller sends a `request` whose command has three lines, quotes and Greek letters | One request event, with the same `id` and the same command, byte for byte |
| UNIT-CONNECTION-05 | `GATE-07` | Live | The controller sends messages of the types `approve`, `approved`, `yes`, `extend`, `settings`, `started`, `ended`, one unknown type and one with no type. Then a `request` with the extra fields "approved" and "answer" | No event for the first ones. The last gives a plain request event, the same as with no extra field |
| UNIT-CONNECTION-06 | `LIM-02` | Live | The controller sends `wait` and `stop` with an `id`, and each once more with no `id` | One wait event and one stop event, each with the `id`. Nothing for the ones with no `id` |
| UNIT-CONNECTION-07 | `GATE-12`, `LIM-07` | Live | The controller sends `cancel` with an `id` | One cancel event with that `id` |
| UNIT-CONNECTION-08 | `GATE-01`, `GATE-02` | Live | Send `started` for A and `refused` for B with the reason `user` | The controller gets both, in that order, with the right `id` and reason |
| UNIT-CONNECTION-09 | `LIM-01`, `SHOW-03` | Live | Send `output`, `still_running`, and `ended` three times: `exit` with code 3, `exit` with code 0, and `killed_by_user` with replaced bytes | The controller gets each. `exit_code` is there for both exits, also when it is 0. It is left out for the kill. `bytes_replaced` is true only for the last |
| UNIT-CONNECTION-10 | `SHOW-03` | Live | Send `output` with an Escape, a zero byte, quotes, a backslash, Greek letters and the mark for replaced bytes | The controller gets the same text, character for character |
| UNIT-CONNECTION-11 | `SHOW-02` | Live | Send two thousand pieces of output, one after the other | The controller gets all of them, in the order they were sent |
| UNIT-CONNECTION-12 | `SES-05`, `SES-07` | Live | The controller drops the connection. Then the cli-tool sends more, and closes | One lost event and no other. The sends and the close come back at once and nothing breaks |
| UNIT-CONNECTION-13 | `SES-02` | Live, with messages sent just before | Close with the reason `user`. Then send more | The controller gets the earlier messages, then `session_end` with `user` as the last one, then sees a proper close of the websocket, not a cut. The close comes back within a second. The loop gets no lost event |
| UNIT-CONNECTION-14 | `SES-03` | Live | The controller sends `session_end` with a reason, then a `request` | One event: the controller ends the session, with that reason. No request event and no lost event. The controller sees the connection close |
| UNIT-CONNECTION-15 | `GATE-01` | Live | The controller sends a `request` of the kind `read`, one with no kind, and one with no `id` | No event. The controller gets `refused` with `unknown_kind` for the first two, and nothing for the third |
| UNIT-CONNECTION-16 | `GATE-07` | Live | The controller sends text that is not JSON, a JSON list, and a message that is not text. Then a `request` | No event for the first three. The connection lives: the request event comes |
| UNIT-CONNECTION-17 | `LIM-10` | Live, a controller that reads nothing, the limit of waiting bytes set low | Send output until the limit is passed | Every send comes back at once. Then one lost event, which says the controller does not take what is sent |
| UNIT-CONNECTION-18 | `SES-05` | Live | The controller sends a message larger than 1 MB | One lost event |
| UNIT-CONNECTION-19 | `LIM-10` | Live, a controller that reads nothing. Once with nothing waiting to be sent, once with far more than the network takes | Close. While it is under way, the controller sends a `request` | The close comes back within its short wait both times. The loop gets no request event and no lost event |
| UNIT-CONNECTION-20 | `KEY-10` | A controller that answers `session_ok` with an API address. Again with none, and with one that holds a cursor code | Dial | The dial gives the address back as it came. It is not checked here: `session` checks it |
| UNIT-CONNECTION-21 | `HOST-08` | A road that counts its calls | Dial over that road | The one call to the controller went over it |
| UNIT-CONNECTION-22 | `LEN-01`, `LEN-03` | Live | Send `time_up`, then `extended` with 600 seconds | The controller gets both, in that order, the second with 600 seconds left |
| UNIT-CONNECTION-23 | `LEN-09` | Live | The controller sends `extended` with seconds, `time_up`, `extend`, and `session_ok` with a length. Then a `request` | No event for the first four. The request event comes |

## `settings`

`settings` gives every other piece its values. It holds no state: it reads once, at start.

### Where the values come from

1. The starting values, fixed in the code. They are the ones in the "Settings" table of `requirements.md`.
2. The user's file, `~/.config/gangway/settings`, if it is there. A value in it takes the place of the starting value. If `XDG_CONFIG_HOME` is set, the file is `gangway/settings` under that folder.
3. The start option `--length`, which `start` reads. It takes the place of `SESSION_LENGTH` for one session.
4. The admin file, `/etc/gangway/settings`, if it is there. It holds bounds, not values: what the layers above ask for is cut to them.

The folders that may be granted are not built yet, so the admin file holds one bound: `SESSION_MAX`.

### The file

```
# Lines that start with # are notes.
COMMAND_TIME_LIMIT = 90s
MAX_WAITING_REQUESTS = 10
```

- One setting on a line: the name, `=`, the value. Spaces around them do not count. Empty lines are skipped.
- A time is a number with a unit: `500ms`, `90s`, `2m`.

### The admin file

```
# The longest a session may last, extensions included.
SESSION_MAX = 2h
```

- It is written like the user's file. Only `SESSION_MAX` may be in it: a time, more than zero.
- With no `/etc/gangway/` folder, or no file in it, there is no bound. (`SET-03`)
- The folder and the file must be owned by root, and nobody but root may be able to write them. The folder must be a real folder, not a link, and the file must be a plain file.
- A length above `SESSION_MAX` is cut to it, and `settings` tells that it was cut, so the screen can say so. A length at or below it is left alone. (`SET-01`, `SET-04`)

### The settings of the MVP

| Name | Kind | Must be |
|---|---|---|
| `CONTROLLER_TAILSCALE_NAME` | Name | Lower-case letters, digits and `-` |
| `JOIN_TIMEOUT` | Time | More than zero |
| `COMMAND_SHELL` | Path | A full path, from `/` |
| `COMMAND_TIME_LIMIT` | Time | More than zero |
| `QUESTION_WAIT` | Time | More than zero |
| `KILL_WAIT` | Time | More than zero |
| `PROMPT_MIN_SHOW` | Time | Zero or more |
| `DOUBLE_CTRL_C_WINDOW` | Time | More than zero |
| `MAX_WAITING_REQUESTS` | Number | One or more |
| `SESSION_LENGTH` | Time | More than zero |
| `EXTEND_WAIT` | Time | More than zero |

### Rules the code must keep

- **A file with a mistake stops the start.** An unknown name, a name set twice, a line with no `=`, or a value that does not fit is an error that names the line. The cli-tool does not start on a guess: a mistyped name would otherwise leave a limit at a value the user did not mean.
- **All or nothing.** When the file has a mistake, no value from it is used.
- **The file is only read.** `settings` never writes it and never makes it. The same holds for the admin file and its folder: the code that reads them opens one file, for reading, and uses nothing that can write. (`SET-02`)
- **The place of the admin file is fixed in the program.** No setting, no start option and nothing in the surroundings of the program moves it or switches it off. An approved command runs as the user and could set any of those. (`SET-01`)
- **The user's file cannot hold `SESSION_MAX`.** It is an error there, with the line, and the error names the admin file. (`SET-01`)
- **An admin file that cannot be trusted or read stops the start.** A wrong owner, a folder or file that others can write, a file that cannot be read, and a mistake in the file are each an error. The cli-tool does not go on as if there were no bound: the file may hold one. (`SET-05`)
- **The acceptance tests use a build of their own.** They cannot write `/etc/gangway/` and do not run as root. A program built with the tag `gangwaytest` takes the folder from `GANGWAY_TEST_ADMIN_FOLDER` and takes the user who runs it as the owner. No other build holds that code. Such a build must never be put on a host.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-SETTINGS-01 | `LIM-06` | No file | Read | Every value is the starting value from the "Settings" table of `requirements.md`. The test reads that table and compares |
| UNIT-SETTINGS-02 | `LIM-06` | A file that sets every setting to another value | Read | Every value is the one from the file |
| UNIT-SETTINGS-03 | `LIM-06` | A file with notes, empty lines, and spaces around names and values | Read | The values are read. The rest is skipped |
| UNIT-SETTINGS-04 | `LIM-06` | A file with one good line and one unknown name | Read | An error that names the line and the name. No value from the file is used |
| UNIT-SETTINGS-05 | `LIM-06` | In turn, a file with: a time with no unit, a time of zero, a time below zero, a word in place of a number | Read | An error each time, with the line |
| UNIT-SETTINGS-06 | `GATE-11` | A file where `COMMAND_SHELL` is `sh`, with no folder | Read | An error |
| UNIT-SETTINGS-07 | `LIM-06` | A file that sets one name twice | Read | An error that names the second line |
| UNIT-SETTINGS-08 | `LIM-06` | A file with a line that has no `=` | Read | An error |
| UNIT-SETTINGS-09 | `LIM-06` | `XDG_CONFIG_HOME` set, then not set | Ask where the file is | `gangway/settings` under that folder, then under `.config` in the home folder |
| UNIT-SETTINGS-10 | `GATE-13` | A file where `MAX_WAITING_REQUESTS` is 0 | Read | An error |
| UNIT-SETTINGS-11 | `GATE-14` | A file where `PROMPT_MIN_SHOW` is `0s` | Read | No error. The value is zero |
| UNIT-SETTINGS-12 | `HOST-06` | A file where `CONTROLLER_TAILSCALE_NAME` is, one by one: empty, with a capital, with a space, an address, a name with a dot, a name that starts or ends with `-` | Read | An error each time |
| UNIT-SETTINGS-13 | `HOST-08` | A file that sets `CONTROLLER_ADDRESS` | Read | An error that names the line and the name: the cli-tool has no such setting any more |
| UNIT-SETTINGS-14 | `LEN-01`, `LEN-04` | A file where `SESSION_LENGTH` is `10m` and `EXTEND_WAIT` is `20s`. Then, for each of the two: `0s`, a time below zero, a number with no unit | Read | Ten minutes and twenty seconds. Then an error each time |
| UNIT-SETTINGS-15 | `SET-01`, `LEN-05` | An admin folder whose file has a note, an empty line and `SESSION_MAX = 20m` with spaces around it | Read | A maximum of 20 minutes, and the path of the file |
| UNIT-SETTINGS-16 | `SET-03` | No admin folder. A folder with no file. A file with a note only | Read. Ask for a length of 100 hours | No error and no maximum each time. The length is not cut |
| UNIT-SETTINGS-17 | `SET-05` | In turn, an admin file with: `SESSION_LENGTH`, a setting of the user's file after a good line, an unknown name, a maximum of zero, below zero, with no unit, with no value, a line with no `=`, `SESSION_MAX` twice | Read | An error each time that names the file and the line. No maximum from the file is used |
| UNIT-SETTINGS-18 | `SET-05` | An admin folder and file owned by the user who runs the test | Read, with another user as the owner that is asked for, and with root. Check the file alone the same way | An error each time that names the path and says who must own it. With the user itself as owner, no error |
| UNIT-SETTINGS-19 | `SET-05` | In turn: the folder writable by its group, by others, by both. The file the same. The file readable by all, by its owner only, and writable by nobody. The file with no rights. A folder in the place of the file. A link in the place of the folder | Read | An error for the first six, that names the path. No error for the next three. An error for a file that cannot be read, for a file that is not a plain file, and for a folder that is a link |
| UNIT-SETTINGS-20 | `SET-01`, `SET-04` | A maximum of 20 minutes | Ask for one hour, for a nanosecond above 20 minutes, for 20 minutes, for 10 minutes | 20 minutes and "cut" for the first two. The length asked for and "not cut" for the others |
| UNIT-SETTINGS-21 | `SET-01` | A user's file that sets `SESSION_MAX` | Read | An error that names the line, the name and the admin file |
| UNIT-SETTINGS-22 | `SET-01`, `SET-02` | `GANGWAY_TEST_ADMIN_FOLDER` is set. The code of `settings` | Ask where the admin folder is. Search the code that reads it | `/etc/gangway`, owned by root, whatever the surroundings say. The file that fixes the place uses no package, and the test build's file is in the test build only. The reading code does not read the surroundings, opens one file for reading, and uses nothing that can write, make, remove, rename or change the rights of a file |

## `loop`

The `loop` ties the pieces of the cli-tool together. Every event is handed to it, one at a time, and it passes the event to the piece it belongs to. It also ends the session. No two events are ever handled at once.

### What it holds

- The pieces: `terminal`, `line`, `gate`, `runner`, the `session` and the `connection`.
- Its state, and why the session ended.
- The length the session starts with: the one asked for at start, or `SESSION_LENGTH`. A length above `SESSION_MAX` is cut to it. It starts the `session`'s clock with the length and the maximum.
- The length before it was cut, and the admin file's path, for the screen.

### States

| State | Meaning |
|---|---|
| `Live` | Requests are taken |
| `Ending` | The session has ended. A command is still being stopped |
| `Over` | Nothing is left to do. The program can end |

```mermaid
stateDiagram-v2
  [*] --> Live
  Live --> Over: the session ends, nothing runs
  Live --> Ending: the session ends, a command runs
  Ending --> Over: the command is gone
  Over --> [*]
```

### Where each event goes

| Event | From | In `Live` it goes to |
|---|---|---|
| A request | `connection` | The `session` first. Its time is up: `refused` with `time_up` is sent and shown. Else `checks`. Refused there: `refused` is sent and the reason is shown. Else `line`. Full: `refused` with `line_full` is sent and shown. Else the `gate` is told one waits |
| `wait`, `stop` | `connection` | The `runner`, when the `id` is the one that runs. Else nowhere |
| `cancel` | `connection` | `line` first: a request found there is removed, and `refused` with `ai_gone` is sent. Else the `gate` and the `runner`, which act only on their own `id` |
| Bytes from the keyboard | The keyboard | `terminal`, which makes `key`, `ctrl_c` and `quit` of them |
| `key` | `terminal` | The `session` while the question to extend waits. Else the `gate` |
| `ctrl_c` | `terminal` | The `session` while the question to extend waits. The `gate` when a prompt waits. The `runner` when a command runs. With nothing in front, the session ends |
| `quit` | `terminal` | The session ends |
| Output, the end of the shell, a timer | The starter, the timers | The `runner`, in every state. After the end of the shell, the `session` is told that no command runs |
| A session timer | The session's timers | The `session`. Then, when no command runs, the `session` is told so |
| The controller ends the session, or the connection is lost | `connection` | The session ends |

### When the session's time is up

The `session` decides it, and the loop does this, in order:

1. Every request in the `line` is refused with `time_up`.
2. The `gate` is told. A prompt that waits is refused with `time_up`, and nothing has run.
3. The `runner` is told. A command that runs is not killed. It gets no more time.
4. When a command still runs, the screen says that the time is up and that the command gets no more time.
5. When no command runs, now or later, the `session` is told. It shows the question to extend, or at the maximum it tells the loop to end the session.

From here on a new request is refused with `time_up`, until the user extends. When the user does not, or when the maximum is reached, the `session` tells the loop, and the session ends with the reason `time_up`. The screen says which of the two it was.

### Ending the session

In this order, once:

1. The `session` is ended, so its key is dead.
2. The `gate` is ended. A prompt that waits is refused with `session_ended`, and is not shown again.
3. Every request in the `line` is refused with `session_ended`.
4. The screen says why the session ends.
5. The `runner` is told. A command that runs is stopped, by asking and then by force.
6. When no command runs any more, the `connection` is closed with the reason, and the loop is `Over`.

### Rules the code must keep

- **After the end, nothing is taken.** In `Ending` and `Over`, a request, a `cancel`, a key and a second end do nothing. Only what the stopping command still needs gets through: its output, its end and its timers.
- **The program does not end while a command runs.** The loop is `Over` only when the `runner` is idle, so no command is left behind by a session that ended. (`SES-05`)
- **The loop decides nothing about a request.** It only passes it on. The yes comes from the `gate` alone.
- **The loop decides nothing about the time.** It hands the `session` its timers, its keys and the news that no command runs. No event from the `connection` reaches the `session`'s clock. (`LEN-09`)

### Unit tests

The pieces are the real ones. The test holds the edges: the keyboard, the screen, the clock, the starter, the timers and the connection. The integration tests `PIE-01`, `PIE-02`, `PIE-04`, `PIE-05`, `PIE-06`, `PIE-12`, `PIE-13` and `PIE-14` of `architecture.md` run in the same way, as Go tests that carry those names.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-LOOP-01 | `GATE-01`, `SHOW-01` | Live | A request, `y`, output, the command ends | `started`, `output`, `ended` are sent in that order. The screen shows the prompt, the `y` and the output, and nothing else |
| UNIT-LOOP-02 | `GATE-13`, `SHOW-01` | Live, limit 2 | Three requests | The third gets `refused` with `line_full`. The screen says why, and the first prompt is again the last thing on it |
| UNIT-LOOP-03 | `LIM-09` | A prompt waits, a second request behind it | Ctrl+C | The first is refused with `user`. The second prompt shows. The session is live |
| UNIT-LOOP-04 | `LIM-08` | A command runs | Ctrl+C, then the command ends | The command was asked to stop. `ended` with `killed_by_user`. The session is live and a new request gets a prompt |
| UNIT-LOOP-05 | `SES-02`, `KEY-03` | Live, nothing in front | Ctrl+C | `Over`. The connection is closed with `user`. The session is ended |
| UNIT-LOOP-06 | `LIM-10`, `GATE-01` | A prompt waits, a second request behind it | Two Ctrl+C | Both requests are refused. Nothing ran. `Over` |
| UNIT-LOOP-07 | `LIM-10`, `SES-05` | A command runs, a request waits | Two Ctrl+C, then the command ends | The command was asked to stop once. `Ending` until it is gone, then `Over` and the connection is closed. The waiting request was refused and got no prompt |
| UNIT-LOOP-08 | `SES-05`, `GATE-01` | A prompt waits. Again with a command that runs | The connection is lost | Nothing runs, and the screen says why. The running command is stopped, and the loop is `Over` when it is gone |
| UNIT-LOOP-09 | `SES-03` | `Over` | A request, keys, `cancel`, `wait`, `stop`, a second end | Nothing is sent, shown or run |
| UNIT-LOOP-10 | `LIM-02` | The wait-or-stop question is asked | `stop` and `wait` with another `id`, and with none. Then with the right `id` | The wrong ones do nothing. The right `wait` starts a new time limit. The right `stop` stops the command |
| UNIT-LOOP-11 | `GATE-12`, `LIM-07` | A command runs, two requests wait | `cancel` for the one in the line, for the one that runs, and for the one at the prompt | Each is closed with `ai_gone`: two with `refused`, the running one with `ended`. Only the first command ever ran. The session is live |
| UNIT-LOOP-12 | `KEY-04` | Live | Begin | The screen shows the paste line with the address and the key |
| UNIT-LOOP-13 | `SES-05` | A prompt waits | The controller ends the session | The request is refused with `session_ended`. The prompt is not shown again. The connection is closed with the controller's reason |
| UNIT-LOOP-14 | `GATE-01`, `GATE-07` | A prompt waits | `wait`, `stop`, timers, output, the end of a shell, and the same request again | Nothing was started. No `started` was sent |
| UNIT-LOOP-15 | `LEN-01`, `LEN-03` | Live, nothing in front | The session timer. Then, after `PROMPT_MIN_SHOW`, `y`. Then a request | `time_up` is sent, and the question to extend is the last thing on screen. After `y`: `extended` is sent with the length, and the request gets a prompt |
| UNIT-LOOP-16 | `LEN-06` | The question to extend waits | A request. Then a request with a control character | Each is refused with `time_up`. No prompt is shown. The screen says why, and the question is again the last thing on it |
| UNIT-LOOP-17 | `LEN-07`, `LEN-03` | A prompt waits | The session timer. Then `y` with no time passed | The request is refused with `time_up` and nothing ran. The screen says the time is up, then shows the question. The `y` did not extend: no `extended` was sent |
| UNIT-LOOP-18 | `LEN-08` | A command runs | The session timer. Then the command's timer. Then the command ends | `time_up` is sent and the command is not asked to stop. No question yet. At its timer it is asked to stop, and no `still_running` is sent. `ended` with `killed_time_up`. Only then the question is shown |
| UNIT-LOOP-19 | `LEN-08` | A command runs | The session timer. Then the command writes and ends by itself | The output and `ended` with `exit` are sent as usual. Then the question is shown, under the output |
| UNIT-LOOP-20 | `LEN-04`, `KEY-03` | The question to extend waits, three times | `n`. Ctrl+C. The session timer of `EXTEND_WAIT` | Each time: `Over`, the session is ended, and the connection is closed with the reason `time_up` |
| UNIT-LOOP-21 | `LEN-03` | The question to extend waits | `Y`, Enter, an arrow key, `x` | The question still waits. Nothing was sent |
| UNIT-LOOP-22 | `LEN-09` | Live | Every event the connection can hand over: a request, `wait`, `stop`, `cancel`. Then the same while the question to extend waits | No session timer was started by any of them, and `extended` was not sent. The question still waits |
| UNIT-LOOP-23 | `LEN-06` | Live. The clock is moved past the end time, and no session timer comes | A request | It is refused with `time_up`, and the question to extend is shown |
| UNIT-LOOP-24 | `LEN-03` | A session that was extended once | The session timers of before the extension. Then the new one | The old ones do nothing. The new one: `time_up` is sent again, and the question is shown again |
| UNIT-LOOP-25 | `LEN-01`, `LEN-02` | A loop with no length asked for. Again with a length of ten minutes | Begin | The session timer was started with `SESSION_LENGTH`, and with ten minutes. The screen says how long the session lasts |
| UNIT-LOOP-26 | `SET-01`, `SET-04` | A maximum of 20 minutes. A loop with 50 minutes asked for at start. Again with 50 minutes as `SESSION_LENGTH`. Again with 10 minutes asked for. Again with no maximum | Begin | The first two: the session timer was started with 20 minutes, and the screen says that 50 minutes is above the maximum, names the admin file, and says it was cut to 20. With 10 minutes: a timer of 10 minutes, no such line, and the screen names the maximum. With no maximum the screen names none |
| UNIT-LOOP-27 | `LEN-05` | A maximum of 40 minutes, a length of 30 | The session timer. `y`. The session timer again. Then a request and `y` | The question names the 10 minutes left and the maximum. After `y`, `extended` carries what is left. At the maximum: `Over`, closed with `time_up`, no second question, and the screen says the session has lasted its maximum. Nothing more is sent |
| UNIT-LOOP-28 | `LEN-05`, `LEN-08` | A session whose length is its maximum. A command runs and a request waits behind it | The session timer. A new request. Then the command ends | The request that waits and the new one are refused with `time_up`. The command is not stopped, and the session is not over while it runs. When it ends: `Over`, closed with `time_up`, and no question was shown |

## `start`

`start` is the beginning and the end of the program. It checks, sets up the edges, runs the `loop`, and cleans up.

### In order

The words after the program's name pick what runs: none for a session over Tailscale, `login` for the `login` piece, `--local` with or without an address for a session in the one-machine mode. A session takes one more option, before or after `--local`: `--length` and a time, such as `--length 45m`. It is the length of this one session, in place of `SESSION_LENGTH`. (`LEN-02`) Anything else stops with a short text on how to start. (`HOST-04`, `HOST-08`)

A session, in order:

1. Refuse to go on as root. Say why. (`HOST-01`)
2. Refuse to go on when the input or the output is not a terminal. Say why. (`HOST-02`)
3. Read the settings: the user's file, then the admin file. A mistake in either stops here, with the line. So does an admin file that cannot be trusted or read. (`SET-05`)
4. Pick the road: Tailscale with the auth key of `login`, or localhost. With no auth key, say to run `login` and stop. (`HOST-06`)
5. `network` joins. On failure, say so and stop.
6. Make the session and its key.
7. Connect to the controller over that road, and tell it the session's length, cut to the maximum if there is one. On failure, say so and stop.
8. Give the API address from `session_ok` to `session`. A bad one stops here, and no paste line is shown. (`KEY-10`)
9. Set the terminal to raw mode.
10. Run the loop until it is `Over`.
11. Put the terminal back. This happens on every way out, also when the program fails. (`HOST-07`)
12. `network` leaves. This happens on every way out after step 5. (`SES-08`)

### Rules the code must keep

- **Nothing is checked later that can be checked first.** The checks of steps 1 and 2 come before the settings are read, before a key is made and before anything is sent.
- **Everything goes through the loop.** The keyboard, the connection, the starter, the timers and the signals each run on their own, and each hands its events to the loop. They touch no piece themselves.
- **A signal from outside ends the session.** `TERM` and `HUP` end it the same way two Ctrl+C do, so a running command is stopped and the terminal is put back.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-START-01 | `HOST-01` | The user is root | Check | An error that says the cli-tool does not run as root |
| UNIT-START-02 | `HOST-02` | The input is not a terminal. Again with the output | Check | An error each time that says a terminal is needed |
| UNIT-START-03 | `HOST-01`, `HOST-02` | A plain user, both are terminals | Check | No error |
| UNIT-START-04 | `HOST-01` | The user is root and there is no terminal | Check | The error is the one about root |
| UNIT-START-05 | `HOST-04`, `HOST-08` | | Read the words after the program's name: none, `login`, `--local`, `--local` with an address | A session over Tailscale, the login, a session in the one-machine mode, the same with that address |
| UNIT-START-06 | `HOST-04` | | Read words that make no sense, such as `start`, `login now`, `-local`, `--help` | An error each time, with the text on how to start |
| UNIT-START-07 | `LEN-02` | | Read: `--length 45m`. `--local --length 90s`. `--length 2h --local` with an address. Then: `--length` with no time, with `0s`, with a time below zero, with a word, with a number that has no unit, given twice, and after `login` | 45 minutes over Tailscale. 90 seconds in the one-machine mode. 2 hours in the one-machine mode with that address. Then an error each time, with the text on how to start |

## `secrets`

`secrets` keeps the Tailscale auth key on the host. It holds no state: it reads and writes one file.

### The file

`~/.config/gangway/tailscale-auth-key`. If `XDG_CONFIG_HOME` is set, the file is `gangway/tailscale-auth-key` under that folder. It holds the key and a line break, and nothing else.

### What it can be asked

| Ask | Does | Gives back |
|---|---|---|
| `save`, with a key | Makes the folder if it is not there, with rights for the owner only. Writes the key to a new file with rights for the owner only, then puts that file in the place of the old one | An error, or nothing |
| `load` | Reads the key | The key, or an error that says to run `login` |
| `remove` | Takes the file away. `login` uses it when Tailscale refuses a new key | An error, or nothing |
| `protected`, with a path | Tells whether the path is the auth key file | Yes or no |

### Rules the code must keep

- **Owner only, from the first moment.** The file is made with its rights already set. It is never made open and closed after. (`HOST-05`)
- **A file that others can read is refused.** `load` gives an error when the group or anyone else can read or write the file, and says how to fix it. (`HOST-05`)
- **The key never reaches the screen or a message.** `secrets` gives the key to `network` and to nothing else. An error names the file, never the key.
- **A key that cannot be one is refused at `save`.** It must start with `tskey-auth-` and hold no space and no control character. A paste of the wrong thing is caught before it is stored.
- **`protected` follows links.** A path that leads to the file through a link or through `..` is the file. The check of file reads uses it when file reads are built. (`KEY-08`)

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-SECRETS-01 | `HOST-05` | No folder, no file | `save` a key | The folder and the file are there. Only the owner can read or write each. `load` gives the key back |
| UNIT-SECRETS-02 | `HOST-05` | A file with an old key | `save` a new key | `load` gives the new key. The rights are for the owner only |
| UNIT-SECRETS-03 | `HOST-04` | No file | `load` | An error that says to run `login` |
| UNIT-SECRETS-04 | `HOST-05` | A file that the group can read. Again one that anyone can read | `load` | An error each time that names the file and the fix. No key is given |
| UNIT-SECRETS-05 | `HOST-05` | Nothing | `save`, one by one: an empty key, a key with a space, one with a control character, one that does not start with `tskey-auth-` | An error each time. No file is made |
| UNIT-SECRETS-06 | `HOST-05` | A file with a key | `load` fails, and `save` fails on a folder that cannot be written | Neither error holds the key |
| UNIT-SECRETS-07 | `KEY-08` | A file with a key | `protected` with: the file's path, a path to it through `..`, a link to it, another file in the same folder | Yes, yes, yes, no |
| UNIT-SECRETS-08 | `HOST-06` | A text with a cursor code, a line break and a letter that is not plain; a text of 500 letters | `plain` | Each of the three is replaced by `?`. The long text is cut to 200 with `...` at the end |

## `network`

`network` is the road to the controller. It has two forms, picked at start, and the rest of the cli-tool cannot tell them apart.

| Form | When | The road |
|---|---|---|
| Tailscale | Always, unless the cli-tool was started with `--local` | Joins the Tailscale network from inside the program and dials the controller by its Tailscale name |
| One machine | Started with `--local` | Dials the controller on localhost. No Tailscale, no auth key |

### What it holds

- In the Tailscale form: the Tailscale device of this start, and its folder.
- In the one-machine form: nothing.

### What it can be asked

| Ask | Does | Gives back |
|---|---|---|
| `join` | Tailscale: joins the network with the auth key and waits until it is up. One machine: nothing | An error, or nothing |
| `dialer` | | What `connection` needs to open its websocket over this road, and the address to dial |
| `leave` | Tailscale: leaves the network and removes the folder. One machine: nothing | |

### The Tailscale form

It uses `tsnet`, Tailscale's library for a device inside a Go program. Source: https://pkg.go.dev/tailscale.com/tsnet

- **A new device at every start.** The device's identity is kept in memory. No start can pick up the device of an earlier one. (`HOST-09`)
- **Ephemeral.** The device joins as ephemeral, so Tailscale removes it after it goes offline.
- **A folder that goes away.** The library needs a folder that exists. `network` makes an empty one for this start, with rights for the owner only, and removes it at `leave`. (`HOST-09`)
- **The name.** The device shows the host's name with `gangway-` in front, so the user can tell it from the host's own Tailscale device, if it has one.
- **The address to dial** is `http://` and `CONTROLLER_TAILSCALE_NAME`, at the hosts' port. The port is fixed in the code both programs share.
- **It takes no connection from outside.** `network` never asks the library to listen. The library holds UDP ports for the tunnel, which is how Tailscale sends and receives. It also holds one TCP port for calls from the host itself, tied to the loopback device, so no other machine can reach it. (`HOST-03`)
- **The library's log lines are not sent to Tailscale.** The library would upload them by default. `network` switches that off before it joins.
- **Only a short-lived device can live in memory.** The library refuses a memory-only identity for any other kind, which fits: the host's device is both.
- **The library's own log lines stay off the screen.** Only `terminal` writes to the screen.

### The one-machine form

- **Localhost only.** The address is `http://127.0.0.1` at the controller's starting port, or the address given with `--local`. An address that is not localhost stops the start, with the reason. (`HOST-08`)
- **No auth key is read.** `secrets` is not asked.

### Rules the code must keep

- **Join before anything is shown as live.** The cli-tool says it is connecting, joins, and only then opens the websocket. A join that fails ends the start with the reason: no auth key, a key that Tailscale refuses, or no answer in `JOIN_TIMEOUT`.
- **`leave` runs on every way out.** After the session ends, when the start fails after `join`, and when the program is told to stop from outside. (`SES-08`)
- **`leave` does not hang.** It gives up after a few seconds. The device is ephemeral, so Tailscale removes it in any case.
- **The form is picked once.** Nothing switches the road while the program runs.

### Unit tests

The Tailscale form is tried with the library pointed at a stand-in for Tailscale's servers, which the library's own test tools give. A test against the real network is not part of the unit tests.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-NETWORK-01 | `HOST-08` | One-machine form, no address given | `dialer` | The address is `http://127.0.0.1` at the controller's starting port |
| UNIT-NETWORK-02 | `HOST-08` | One-machine form | Make it with, one by one: `http://localhost:9000`, `http://127.0.0.1:9000`, `http://[::1]:9000` | Each is taken |
| UNIT-NETWORK-03 | `HOST-08` | One-machine form | Make it with, one by one: `http://192.168.1.5:9000`, `http://example.com`, `http://0.0.0.0:9000`, text that is no address | An error each time that says the mode is for localhost only |
| UNIT-NETWORK-04 | `HOST-08` | One-machine form, no auth key file | `join` | No error. `secrets` was not asked |
| UNIT-NETWORK-05 | `HOST-06` | Tailscale form, a stand-in for Tailscale's servers, a good key | `join` | No error. The stand-in has one new device, with the host's name and `gangway-` in front. The library was asked for an ephemeral device |
| UNIT-NETWORK-06 | `HOST-06` | State after UNIT-NETWORK-05 | `dialer` | The address is `http://`, `CONTROLLER_TAILSCALE_NAME` and the hosts' port |
| UNIT-NETWORK-07 | `HOST-09` | The same stand-in | `join`, `leave`, then a new `network` and `join` again | The stand-in saw two devices with two identities |
| UNIT-NETWORK-08 | `HOST-09`, `SES-08` | State after UNIT-NETWORK-05 | `leave` | The folder of this start is gone. The device no longer answers |
| UNIT-NETWORK-09 | `HOST-06` | Tailscale form, a stand-in that refuses the key | `join` | An error that says Tailscale refused the key and to run `login` again, with what Tailscale said. The error does not hold the key |
| UNIT-NETWORK-10 | `HOST-06` | Tailscale form, a stand-in that never answers | `join` | An error after `JOIN_TIMEOUT`. The folder of this start is gone |
| UNIT-NETWORK-11 | `HOST-03` | State after UNIT-NETWORK-05 | List the TCP ports this program listens on | Each is on a localhost address or tied to the loopback device. A port the test opens to all is seen by the same check |
| UNIT-NETWORK-12 | `SES-08` | A stand-in that never answers `leave` | `leave` | It comes back within a few seconds |

## `login`

`gangway login` stores the Tailscale auth key. It is run once, and again when the key has run out.

### In order

1. Refuse to go on as root, or with no terminal, as at a normal start. (`HOST-01`, `HOST-02`)
2. Ask for the key. What is typed or pasted is not shown.
3. `secrets` saves it. A key that cannot be one stops here.
4. `network` joins with it, to prove that Tailscale takes it, and leaves again.
5. Say that the host is ready, and how to start a session.

### Rules the code must keep

- **The key is never shown.** Not while it is pasted, not in a message, not in an error.
- **A key that Tailscale refuses is not kept.** When step 4 fails because of the key, the file is removed again, and the old key, if there was one, is put back.
- **A network that is down is not the key's fault.** When step 4 fails for another reason, the key is kept, and the message says that it could not be tried.
- **Pairing comes later.** `login` does not show a pairing code until pairing is built.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-LOGIN-01 | `HOST-04` | No key file. Tailscale takes the key | Run `login` and paste a key | The key is stored. The screen says the host is ready. `network` joined and left |
| UNIT-LOGIN-02 | `HOST-05` | The same | Run `login` and paste a key | The key is on no part of the screen |
| UNIT-LOGIN-03 | `HOST-05` | An old key is stored. Tailscale refuses the new one | Run `login` and paste the new key | The screen says Tailscale refused it. The old key is still the one stored |
| UNIT-LOGIN-04 | `HOST-05` | No key file. Tailscale refuses the key | Run `login` and paste a key | No key file is left |
| UNIT-LOGIN-05 | `HOST-04` | No key file. Tailscale does not answer | Run `login` and paste a key | The key is stored. The screen says it could not be tried |
| UNIT-LOGIN-06 | `HOST-05` | No key file | Run `login` and paste text that cannot be a key | An error. No file. `network` was not asked |
| UNIT-LOGIN-07 | `HOST-01`, `HOST-02` | The user is root. Again with no terminal | Run `login` | It refuses and asks for no key |

## `sessions` (controller)

`sessions` holds the live sessions, in memory only.

### What it holds

For each live session: the hash of its key, the host's name, the limits the host gave, the session's `relay`, and, once a caller has claimed the key, the hash of the caller token. It never holds a key or a caller token.

It also holds what the host said about its time: when the time will be up, and whether it is up now. And it holds the controller's clock, which a test replaces.

### What it can be asked

| Ask | Does | Gives back |
|---|---|---|
| `add`, with a session | Makes the session live | Yes, or no with the reason |
| `find`, with a key | Hashes the key and looks the hash up | The session, or nothing |
| `remove`, with a session | Takes the session out | |
| `claim`, on a session | Makes the caller the owner of the key | A new caller token the first time. Nothing every time after |
| `owns`, on a session, with a token | Hashes the token and compares | Yes or no |
| `counts`, on a session, with the time now and a length | Notes that the session's time is up after that length | |
| `time is up`, on a session | Notes that the host said so | |
| `extended`, on a session, with the time now and the time left | Notes the new end, and that the time is no longer up | |
| `time`, on a session, with the time now | Reads | The whole seconds left, rounded up, and whether the time is up |

### Rules the code must keep

- **Only a hash is held.** `add` takes the hash the host sent. `find` hashes the key an AI shows and compares. No function takes a key and keeps it. (`SES-06`)
- **A hash has one form.** 64 characters, the digits and `a` to `f` only. Anything else is refused, so an empty or short hash can never match a key.
- **One live session for one hash.** `add` refuses a hash that a live session already has. A second cli-tool could otherwise take over the requests of the first. (`KEY-09`)
- **A key must start with `gw_`.** `find` gives nothing for any other key and does not hash it.
- **Removed means dead at once.** After `remove`, `find` gives nothing for that key. (`KEY-03`)
- **Removing an old session does not harm a new one.** `remove` takes out only the very session it was given, not another session that came later with the same hash.
- **Nothing is written to disk.** A new store is empty. (`SES-06`, `SES-07`)
- **One caller token for one session.** The first `claim` makes a token of 256 random bits, as many as the session key has, with `gwc_` in front. Every later `claim` gives nothing, also when many come at the same moment: the check and the making happen under one lock. (`KEY-05`)
- **Only the token's hash is held.** The token itself leaves in the answer to `claim` and is kept nowhere. `owns` hashes what it is shown and compares the two hashes in a time that does not depend on how much of a guess was right. (`KEY-05`, `SES-06`)
- **No claim, no owner.** Before a `claim`, `owns` says no to everything, an empty token too. (`KEY-05`)
- **The token dies with the session.** It is part of the session and of nothing else. A new session with the same key hash starts with no owner. (`KEY-05`, `KEY-03`)
- **The time is only noted.** The end time and "the time is up" are what the host said. `sessions` refuses nothing and ends nothing because of them: the cli-tool does both, and only its user extends. (`LEN-09`)
- **The time is up only when the host says so.** When the end time passes with no word from the host, the seconds left are 0 and the time is not yet up.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-SESSIONS-01 | `KEY-02` | An empty store | `add` a session with the hash of key K. `find` with K | The session |
| UNIT-SESSIONS-02 | `KEY-02`, `SES-01` | A store with the session of key K | `find` with: no key, a made-up key, K with one letter cut off, K with one letter added, K without `gw_`, the hash itself. Then a key with no `gw_` whose hash a session has | Nothing, each time |
| UNIT-SESSIONS-03 | `KEY-03`, `SES-03` | A store with the session of key K | `remove` it. `find` with K | Nothing |
| UNIT-SESSIONS-04 | `KEY-09` | A store with the session of key K | `add` a second session with the same hash | Refused. `find` with K still gives the first |
| UNIT-SESSIONS-05 | `KEY-09` | An empty store | `add` with a hash that is empty, too short, too long, in capitals, or not hex | Refused each time. The store is still empty |
| UNIT-SESSIONS-06 | `KEY-03` | Session A was removed. Session B was added with the same hash | `remove` A once more | `find` still gives B |
| UNIT-SESSIONS-07 | `SES-07` | | Make a new store | It holds no session |
| UNIT-SESSIONS-08 | `SES-06` | A store with one session | Look at every field of the session | The key is in none of them |
| UNIT-SESSIONS-09 | `KEY-05` | A live session | `claim` twice. Then `claim` on a second session | The first gives a token: `gwc_` and 256 bits in hex. The second gives nothing, and the first token still owns the session. The other session gets another token |
| UNIT-SESSIONS-10 | `KEY-05` | A live session | `owns` before any `claim`, with: nothing, `gwc_` alone, a made-up token. Then `claim`, and `owns` with: the token, nothing, the token with one letter cut off, with one letter added, without `gwc_`, in capitals, the token of another session, the session key, the token's hash | No three times. Then yes for the token, and no for every other |
| UNIT-SESSIONS-11 | `KEY-05` | A live session | 100 `claim` at the same moment | Exactly one gets a token, and that token owns the session |
| UNIT-SESSIONS-12 | `KEY-05`, `SES-06` | A session after `claim` | Look at every field of the session | The token is in none of them, as text or as bytes |
| UNIT-SESSIONS-13 | `KEY-05`, `KEY-03` | A session after `claim` | `remove` it. `add` a new session with the same hash | The old token does not own the new session. The new session gives a token of its own |
| UNIT-SESSIONS-14 | `LEN-01` | A session that `counts` 30 minutes | `time` at once, 10 minutes later, half a second before the end, at the end, and an hour after it | 1800, 1200, 1, 0 and 0 seconds. The time is not up in any of them |
| UNIT-SESSIONS-15 | `LEN-03`, `LEN-06` | State after UNIT-SESSIONS-14 | `time is up`, then `time`. `extended` with 600 seconds, then `time`. `extended` with a time below zero, then `time` | 0 seconds, and the time is up. 600 seconds, and it is not up. 0 seconds, and it is not up |
| UNIT-SESSIONS-16 | `LEN-09` | Two sessions that each count 30 minutes | `extended` with an hour on one. `time is up` on the same one | The time of the other has not moved, and is not up |

## `relay` (controller)

The `relay` ties an open API call to its request on the host. One `relay` serves one session, so a call can only ever touch the requests of its own session.

### What it holds

For each request it still follows: its state, whether the command has started, the output that came since the last answer, and the open call, if there is one.

### States of a request

| State | Meaning |
|---|---|
| `Open` | A call is open and waits for an answer |
| `Asked` | The call was answered with "still running". No call is open. The AI owes a `wait` or a `stop` |
| `Done` | The command ended while no call was open. The result is kept for the next call |

A request that is none of these is not held: it was answered and forgotten.

```mermaid
stateDiagram-v2
  [*] --> Open: a new request
  Open --> [*]: refused, ended, the AI hangs up, or the session ends
  Open --> Asked: still_running
  Asked --> Open: wait or stop
  Asked --> Done: ended
  Asked --> [*]: the session ends
  Done --> [*]: wait or stop, or the session ends
```

### Events

| Event | Comes from |
|---|---|
| `ask` | `api`: a new request |
| `wait`, `stop` | `api`: a call on the `id` of a request |
| `hang up` | `api`: the AI closed its call before the answer |
| `started`, `output`, `still_running`, `refused`, `ended` | `hostdoor`: a message from the host |
| `end` | `hostdoor`: the session is over |

### The table

An empty "Does" means the event is dropped.

| State | Event | Does | Next |
|---|---|---|---|
| not held | `ask` | Makes a new `id`. Sends `request` to the host | `Open` |
| not held | `wait`, `stop` | Tells `api` there is no such command | not held |
| not held | any message from the host | | not held |
| `Open` | `started` | Notes that the command runs | `Open` |
| `Open` | `output` | Keeps the text | `Open` |
| `Open` | `still_running` | Answers the call with `running` and the output kept so far | `Asked` |
| `Open` | `refused` | Answers the call with `refused` and the host's reason | not held |
| `Open` | `ended` | Answers the call with `ended`, the way it ended, and the output kept | not held |
| `Open` | `hang up`, by the call that is open | Sends `cancel` to the host | not held |
| `Open` | `hang up`, by an older call | | `Open` |
| `Open` | `wait`, `stop` | Tells `api` there is no such command | `Open` |
| `Open` | `end` | Answers the call with `refused` and `session_ended` | not held |
| `Asked` | `wait` | Sends `wait` to the host. Opens the new call | `Open` |
| `Asked` | `stop` | Sends `stop` to the host. Opens the new call | `Open` |
| `Asked` | `output` | Keeps the text | `Asked` |
| `Asked` | `ended`, `refused` | Keeps the result | `Done` |
| `Asked` | `still_running`, `hang up` | | `Asked` |
| `Asked` | `end` | | not held |
| `Done` | `wait`, `stop` | Answers the new call at once with the kept result. Sends nothing to the host | not held |
| `Done` | any message from the host | | `Done` |
| `Done` | `end` | | not held |

After `end`, an `ask` is answered at once with `refused` and `session_ended`, and nothing is sent.

### Rules the code must keep

- **The relay never judges a request.** It makes no message that approves, and it never changes a command: the host gets the command letter for letter. (`GATE-05`, `GATE-07`)
- **One call for one request.** While a call is open on a request, a `wait` or `stop` on it is turned away. To stop a command before its time limit, the AI hangs up.
- **Every call gets one answer.** A call ends with exactly one of `refused`, `ended` or `running`, or the AI hangs up first.
- **Output is given once.** An answer holds the output that came since the last answer, and that output is then forgotten.
- **Only an open call can cancel.** After a `running` answer no call is open, so nothing is cancelled. A call that already has its answer cancels nothing either.
- **A host answers only its own requests.** A message with an `id` the relay does not hold is dropped.
- **The `id` is made here.** `r_` and 16 random hex characters. A host or an AI never picks one.
- **Sending never blocks.** The relay hands each message to `hostdoor` and goes on.

### Unit tests

Start: a relay whose messages to the host are gathered in a list.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-RELAY-01 | `GATE-05` | A new relay | `ask` with a command of two lines with a pipe | One `request` was sent, with a new `id`, the kind `command`, and the command letter for letter |
| UNIT-RELAY-02 | `GATE-02` | A request is `Open` | `refused` from the host with the reason `user` | The call gets `refused` with `user`. The request is no longer held |
| UNIT-RELAY-03 | `GATE-01` | A request is `Open` | `started`, two pieces of `output`, then `ended` with `exit` and code 0 | The call gets `ended`, `exit`, code 0, and both pieces joined in order |
| UNIT-RELAY-04 | `SHOW-03` | A request is `Open` | `ended` with `cut` and `bytes_replaced` | Both are true in the answer |
| UNIT-RELAY-05 | `LIM-01` | A request is `Open` | `started`, one `output`, `still_running` | The call gets `running`, the `id` and the output so far. The request is `Asked` |
| UNIT-RELAY-06 | `LIM-02` | A request is `Asked` | `wait`. Then one more `output` and `ended` | `wait` was sent to the host. The new call gets `ended` with the new output only |
| UNIT-RELAY-07 | `LIM-02` | A request is `Asked` | `stop`. Then `ended` with `killed_by_ai` | `stop` was sent. The new call gets `ended` with `killed_by_ai` |
| UNIT-RELAY-08 | `LIM-04` | A request is `Asked` | `output`, then `ended` with `exit`. Then more messages from the host about it. Then `wait` | The call gets `ended` with `exit` and the first output at once: the later messages changed nothing. Nothing was sent to the host. A second `wait` finds no such command |
| UNIT-RELAY-09 | `GATE-12` | A request is `Open`, not started | `hang up` | `cancel` was sent with that `id`. A later `ended` from the host is dropped |
| UNIT-RELAY-10 | `LIM-07` | A request is `Open` and started | `hang up` | `cancel` was sent with that `id` |
| UNIT-RELAY-11 | `LIM-03` | A request is `Asked` | `hang up` by the call that got the `running` answer | Nothing was sent. The request is still `Asked` |
| UNIT-RELAY-12 | `LIM-02` | A request is `Open` | `wait` and `stop` on its `id`. Then the same on a made-up `id` | No such command, each time. Nothing was sent |
| UNIT-RELAY-13 | `GATE-06` | Two requests are `Open` | The host answers the second, then the first | Each call gets its own answer |
| UNIT-RELAY-14 | `KEY-03` | One request is `Open`, one is `Asked`, one is `Done` | `end` | The open call gets `refused` with `session_ended`. Nothing is held. A new `ask` gets `refused` with `session_ended` and sends nothing |
| UNIT-RELAY-15 | `GATE-07` | A request is `Open` | Messages from the host with a made-up `id`, and with made-up types such as `approve` | Nothing changes. The call still waits |
| UNIT-RELAY-16 | `GATE-06` | One request `Open` and not started, one `Open` and started, one `Asked`, one `Done` | Ask for the counts | One waits. One of the two that run is named |
| UNIT-RELAY-17 | `GATE-01` | A new relay | Many goroutines `ask` at once, and the host answers each | Every `id` differs. Every call gets the answer for its own request |

## `hostdoor` (controller)

`hostdoor` takes the connections from the cli-tools. One connection carries one session, from `session_start` to the end.

### Rules the code must keep

- **First `session_start`, or nothing.** The first message must be `session_start` with a key hash, the limits and a length above zero, and it must come within ten seconds. Anything else closes the connection and makes no session.
- **`session_ok` only after the session is live.** The host gets `session_ok` once `sessions` has taken the session. A hash that `sessions` refuses closes the connection. (`KEY-09`)
- **The end of the connection is the end of the session.** A lost connection, a `session_end` from the host, a message that is not JSON, or a message that is too large: each ends the session at once. The key dies first, then every open call is answered. (`KEY-03`, `SES-03`)
- **A host can only answer, tell its time and end.** The door passes `refused`, `started`, `output`, `still_running` and `ended` to the session's own `relay`, notes `time_up` and `extended` on the session of that connection, and takes `session_end`. Every other type is dropped. (`LEN-01`, `LEN-03`)
- **The door sends nothing about the time.** It has no message that extends a session or moves its end. (`LEN-09`)
- **A slow host is cut off.** Messages to a host wait in a short queue. When the queue is full, the connection is closed and the session ends.
- **No web page.** A connection that a browser opens from another site is refused.
- **Shutting down tells the hosts.** Each host gets `session_end` with `controller_stopped`.

- **`session_ok` holds the API address.** It is the controller's `-public-address`, or the address the API listens on when that is not set. The host puts it in its paste line. (`KEY-10`)

There is no host token yet. Any program that can reach the door can start a session. In the one-machine mode the door is on this machine only (`CTL-01`).

### Unit tests

Start: a door behind a test server, and a websocket client in the test that plays the cli-tool.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-HOSTDOOR-01 | `SES-01` | A door | Connect. Send `session_start` with the hash of key K | `session_ok` comes back. `sessions` finds the session with K, with the host's name and limits |
| UNIT-HOSTDOOR-02 | `SES-01` | A door | Connect. Send, as the first message: `started`. Then on new connections: an `output` that carries a key hash and limits, text that is not JSON, `session_start` with no limits, `session_start` with no key hash, and `session_start` with no length | Each connection is closed. No session is live |
| UNIT-HOSTDOOR-03 | `KEY-09` | A live session with the hash of K | A second connection sends `session_start` with the same hash | It is closed with no `session_ok`. The first session still works |
| UNIT-HOSTDOOR-04 | `KEY-03`, `SES-03` | A live session, with one call open on its relay | The client drops the connection | The call gets `refused` with `session_ended`. `sessions` no longer finds K |
| UNIT-HOSTDOOR-05 | `KEY-03` | A live session, with one call open | The client sends `session_end` | The same as above. The connection is closed |
| UNIT-HOSTDOOR-06 | `GATE-01` | A live session, with one call open | The client sends `started`, `output`, `ended` for that `id` | The call gets `ended` with that output |
| UNIT-HOSTDOOR-07 | `GATE-07` | A live session, with one call open | The client sends messages of the types `approve`, `request` and `session_ok` | The call still waits. The session is still live |
| UNIT-HOSTDOOR-08 | `SES-01` | A door with a short start time | Connect and send nothing | The connection is closed. No session is live |
| UNIT-HOSTDOOR-09 | `KEY-03` | A live session, the queue set to two messages | The client reads nothing. The relay sends many requests | The connection is closed and the session is no longer live |
| UNIT-HOSTDOOR-10 | `SES-07` | Two live sessions | Shut the door down | Each client gets `session_end` with `controller_stopped`. No session is live |
| UNIT-HOSTDOOR-11 | `CTL-01` | A door | Connect with the `Origin` of another web site | Refused. No session is live |
| UNIT-HOSTDOOR-12 | `KEY-03` | A live session | The client sends one message larger than the largest allowed | The connection is closed and the session is no longer live |
| UNIT-HOSTDOOR-13 | `KEY-10` | A door with an API address | Connect. Send `session_start` | `session_ok` holds that address |
| UNIT-HOSTDOOR-14 | `LEN-01` | A door whose clock the test holds | Connect. Send `session_start` with a length of 600 seconds. Then on new connections: a length of 0, and a length below zero | The session has 600 seconds left. The other two connections are closed and make no session |
| UNIT-HOSTDOOR-15 | `LEN-03`, `LEN-06` | A live session | The client sends `time_up`. Then `extended` with 300 seconds | The session says its time is up. Then it says 300 seconds are left and the time is not up. It is still live after both |
| UNIT-HOSTDOOR-16 | `LEN-09` | Two live sessions | The client of one sends `extended` with an hour, then `time_up`. Read everything the other client was sent | The time of the other session has not moved. Its client got `session_ok` and nothing else |

## `api` (controller)

`api` is what the AI calls. The calls and their answers are laid out in `architecture.md`.

### Rules the code must keep

- **No key, no entry.** Every call needs the key of a live session in `Authorization: Bearer`. Without it the answer is 401, always with the same body, and nothing else happens. (`KEY-02`, `SES-01`)
- **The first `hello` owns the key.** `hello` asks `sessions` to `claim`. The first one gets the caller token in its answer. Every later `hello` gets 401, with or without the token, so the token is handed out once. (`KEY-05`)
- **No caller token, no entry.** Each of the other four calls needs the token in the `Gangway-Caller` header, next to the key. With none, a wrong one, or before any `hello`, the answer is the same 401 as for a wrong key, nothing reaches the host, and no token is handed out. A token in the address does not count. (`KEY-05`)
- **Five calls and no more.** Every other path or way of calling gets 404 or 405. No call approves, extends or changes a setting. (`GATE-07`)
- **Only the command goes on.** From the body of a new request the API takes the kind and the command. Any other field is left behind, so the host never sees it. (`GATE-07`)
- **A body it does not know is turned away.** A body that is not JSON, has no command, is too large, or has a kind other than `command` gets 400, and nothing reaches the host. (`CTL-11`)
- **A call stays open** until the `relay` has an answer. When the AI closes it first, the `relay` is told. (`GATE-12`, `LIM-07`)
- **`session` only reads.** It gives the host's name, the seconds left, whether the time is up, the limits, and the counts of requests. `hello` gives the same body with the caller token in front, and changes nothing but the owner of the key.
- **The API tells the time and cannot change it.** The seconds left and "the time is up" come from `sessions`, which holds what the host said. No call, no header and no body moves them. (`LEN-09`)

### Unit tests

Start: the API over a store with one live session of key K. The test plays the host by reading what the session's relay sends. Unless a test says otherwise, the test has said `hello` and sends the caller token with every call.

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-API-01 | `KEY-02`, `SES-01` | A live session | Each of the five calls with: no header, a made-up key, K with one letter cut off, K with no `Bearer`, K in a query string | 401 each time, with the same body. Nothing was sent to the host |
| UNIT-API-02 | `KEY-03` | A live session | The session is removed. Each of the five calls with K | 401 each time |
| UNIT-API-03 | `GATE-07` | A live session | Calls with K to `/v1/requests/x/approve`, `/v1/session/extend`, `/v1/settings`, `/` and `GET /v1/requests` | 404 or 405. Nothing was sent to the host |
| UNIT-API-04 | `CTL-11` | A live session | `POST /v1/requests` with: text that is not JSON, an empty body, no command, an empty command, the kind `read`, a made-up kind, a body larger than the limit | 400 each time. Nothing was sent to the host |
| UNIT-API-05 | `GATE-07`, `GATE-05` | A live session | `POST /v1/requests` with a command and the extra fields `"approved": true` and `"id": "mine"` | The host gets one `request` with the command letter for letter, an `id` of the controller's own, and no other field |
| UNIT-API-06 | `GATE-02` | A request is open | The host answers `refused` with `user` | 200, with `state` `refused` and `reason` `user`, and no other field |
| UNIT-API-07 | `GATE-01` | A request is open | The host sends `output` and `ended` with `exit` and code 0 | 200, with `state` `ended`, `how` `exit`, `exit_code` 0, the output, and `cut` and `bytes_replaced` false |
| UNIT-API-08 | `LIM-01` | A request is open | The host sends `still_running` | 200, with `state` `running`, the `id`, the output so far and `answer_within_seconds` as the host's limit |
| UNIT-API-09 | `LIM-02` | State after UNIT-API-08 | `wait` on the `id`, and the host sends `ended`. Then on a second request, `stop` | The host got `wait`, then `stop`. Each call ends with `ended` |
| UNIT-API-10 | `LIM-02` | A live session | `wait` and `stop` on a made-up `id` | 404 each time. Nothing was sent to the host |
| UNIT-API-11 | `GATE-12`, `LIM-07` | A request is open | The AI closes the call | The host gets `cancel` with the `id` |
| UNIT-API-12 | `GATE-06` | A live session with limits from the host | `GET /v1/session`, and compare with what `hello` gave. Then one request waits and one runs: `GET /v1/session` again | The host's name, the limits, nothing waiting, nothing running. `hello` gave the same body with the caller token in front. Then one waiting, and the `id` of the one that runs |
| UNIT-API-13 | `LIM-08` | A request is open | The host sends `ended` with `killed_by_user` and no exit code | The answer has no `exit_code` field |
| UNIT-API-14 | `KEY-05` | A live session, no `hello` yet | `hello` with K | 200, with a caller token that `sessions` says owns the session, and the state of the session |
| UNIT-API-15 | `KEY-05` | A live session | A second `hello` with K and: no token, the right token, a made-up token. Then `GET /v1/session` as the owner | 401 each time, with the body of a call with no key, and no token in it. The owner is still let in |
| UNIT-API-16 | `KEY-05` | Two live sessions, each after its `hello` | Each of the four other calls with K and: no token, the token with one letter cut off, with one letter added, without `gwc_`, in capitals, the token of the other session, the key, the token's hash, the token in the address | 401 each time, with the body of a call with no key. Nothing was sent to the host |
| UNIT-API-17 | `KEY-05` | A live session, no `hello` yet | Each of the four other calls with K, with no token and with a made-up one. Then `hello` | 401 each time. Nothing was sent to the host. `hello` still gets the caller token |
| UNIT-API-18 | `KEY-05` | A live session | With K and the token: `GET /v1/session`, `wait` and `stop` on a made-up `id`, and `POST /v1/requests` | 200, then 404 twice, and the host gets the request |
| UNIT-API-19 | `KEY-05` | A live session, no `hello` yet | 40 `hello` with K at the same moment | Exactly one gets 200 with a caller token. Every other gets 401 |
| UNIT-API-20 | `LEN-01`, `LEN-06` | A live session that counts 30 minutes, with a clock the test holds | `hello`. Ten minutes pass: `GET /v1/session`. The host's time is up: `GET /v1/session`. The host's user extends by 600 seconds: `GET /v1/session` | `seconds_left` 1800 and `time_up` false. Then 1200. Then 0 and `time_up` true. Then 600 and `time_up` false |
| UNIT-API-21 | `LEN-09` | A live session, with a clock the test holds | Read `seconds_left`. Then, with K and the token: `POST` to `/v1/session/extend`, `/v1/extend` and `/v1/session`, a `GET /v1/session` with a body and a header that ask for an hour more, and a second `hello`. Read `seconds_left` again | 404, 405 or 401 for each, and the `GET` answers as if nothing was asked. Nothing was sent to the host. `seconds_left` is the same as before |

## `start` (controller)

`start` checks the address, puts the pieces together and runs them.

### Rules the code must keep

- **Localhost with no setting.** With no `-listen` option the controller listens on `127.0.0.1:8642`. (`CTL-01`)
- **This machine only, for now.** No login is built, so `start` refuses every address that another machine could reach: only `127.0.0.1`, `::1`, any other loopback address and `localhost` pass. An address with no host, such as `:8642`, means every address and is refused. It prints the reason and does not start. (`CTL-02`)
- **One listener, two doors.** The API and the door for the cli-tools share the address. The path `/host/v1` goes to `hostdoor`, everything else to `api`.

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-CTLSTART-01 | `CTL-01` | | Check the address used with no setting | It passes, and it is a loopback address |
| UNIT-CTLSTART-02 | `CTL-01` | | Check `127.0.0.1:1`, `127.9.9.9:1`, `[::1]:1`, `localhost:1` | Each passes |
| UNIT-CTLSTART-03 | `CTL-02` | | Check `:8642`, `0.0.0.0:8642`, `[::]:8642`, `192.168.1.5:8642`, `100.64.0.1:8642`, `8.8.8.8:80`, `example.com:80`, `8642`, the empty address | Each is refused |
| UNIT-CTLSTART-04 | `CTL-02` | | Run the program with `-listen 0.0.0.0:0` | It prints the reason, gives an exit code that is not 0, and listens on nothing |
| UNIT-CTLSTART-05 | `CTL-01` | The handler of the whole controller | A call to `/v1/session` with no key. A websocket to `/host/v1`. A websocket to other paths, such as `/` and `/host/v1/more` | 401 from the API. The websocket is taken by the door at `/host/v1` and at no other path |
| UNIT-CTLSTART-06 | `CTL-10` | | Run the program with `-local` | The start text says that the one-machine mode is on, and that any program on this machine can start a session |
| UNIT-CTLSTART-07 | `KEY-10` | The program runs with `-local`. Again with `-public-address` set | A host sends `session_start` | `session_ok` holds the address the API listens on. Again: it holds the address that was set |
| UNIT-CTLSTART-08 | `KEY-10` | | Run the program with `-public-address` set to, one by one: a name with no `http`, an address with a space, one that starts with `ftp`, one with a cursor code, `http://` alone | It stops each time, names the option, and listens on nothing |
| UNIT-CTLSTART-09 | `CTL-10` | The two handlers of the Tailscale form | A websocket to `/host/v1` on the API's handler. A call to `/v1/session` on the hosts' handler | Both get "not found" |
| UNIT-CTLSTART-10 | `CTL-12` | No auth key file | Run the program with no option | It says to run `login`, gives an exit code that is not 0, and listens on nothing, not even for the API |
| UNIT-CTLSTART-11 | `CTL-10`, `KEY-10` | A stand-in for Tailscale's servers, a key stored | Run the program with no option. A second device on the stand-in network opens `/host/v1` at `gangway-controller` and sends `session_start`. The same is tried on the API's listener | Over Tailscale, `session_ok` comes back with the API address. On the API's listener: "not found" |
| UNIT-CTLSTART-12 | `CTL-12` | A stand-in for Tailscale's servers. An old key is stored | `Login` with a key the stand-in takes. Again with one it refuses | The new key is stored. Again: the error says Tailscale refused it, and the key from before is still the one stored |
| UNIT-CTLSTART-13 | `CTL-12` | A stand-in for Tailscale's servers, a key stored. The API's address is taken by another program | Run the program | It says why, gives an exit code that is not 0, and has left Tailscale again: its door no longer answers |
| UNIT-CTLSTART-14 | `CTL-12` | | Run the program with words that make no sense, such as `login now`, `start`, an option it does not have | The text on how to start, and the exit code 2. Nothing listens |

## `network` (controller)

`network` (controller) opens the door for the hosts. It has the same two forms as on the cli-tool, picked at start.

| Form | When | The door |
|---|---|---|
| Tailscale | Always, unless the controller was started with `-local` | A listener on the Tailscale network only, at the hosts' port, under the name `gangway-controller` |
| One machine | Started with `-local` | The hosts' path on the same localhost address as the API |

### The Tailscale form

- **The listener comes from the library.** `tsnet` gives a listener that is announced on the Tailscale network only. The door has no address of its own that a setting could get wrong. (`CTL-10`)
- **The API does not answer on that listener, and the hosts' path does not answer on the API's.** Each listener has its own set of paths. (`CTL-10`)
- **The device is kept.** The controller's Tailscale identity lives in `~/.local/state/gangway-controller/`, with rights for the owner only, so its name stays the same after a restart.
- **The auth key is given once**, with `gangway-controller login`, and kept as on the host: its own file in `~/.config/gangway-controller/`, readable by its owner only. (`CTL-12`)
- **A start with no key stops**, and says to run `login`. It does not fall back to the one-machine form. Nothing listens, not even the API.
- **`login` tries the key.** It stores the key, joins once, and leaves. A key that Tailscale refuses is not kept, and the key from before, if there was one, is put back.
- **The library's log lines are not sent to Tailscale**, as on the cli-tool.
- **The name can be set** with `-tailscale-name`, for a network where `gangway-controller` is taken. The hosts then set `CONTROLLER_TAILSCALE_NAME` to the same.

### The one-machine form

- **Localhost only.** The hosts' path answers on the API's listener, and that listener is on localhost: the check of `start` holds. (`CTL-10`)
- **It says so.** At start the controller prints that the one-machine mode is on, and that any program on this machine can start a session. (`CTL-10`)

### Unit tests

| Test | Requirement | Start | Action | Expect |
|---|---|---|---|---|
| UNIT-CTLNETWORK-01 | `CTL-12` | A stand-in for Tailscale's servers, a key stored | Join | The stand-in has one device named `gangway-controller`. The library was not asked for an ephemeral device |
| UNIT-CTLNETWORK-02 | `CTL-10` | State after UNIT-CTLNETWORK-01, the door open | A second device on the stand-in network dials `gangway-controller` at the hosts' port | It reads what the door wrote |
| UNIT-CTLNETWORK-03 | `CTL-10` | State after UNIT-CTLNETWORK-01, the door open | Dial the hosts' port on localhost and on every address of the machine | Refused each time |
| UNIT-CTLNETWORK-04 | `CTL-12` | No auth key file | Join | An error that says to run the controller's `login`. No device joined. No state folder was made |
| UNIT-CTLNETWORK-05 | `CTL-12` | A key stored | Join, close, join again with the same folders | The same identity both times. The stand-in has one device |
| UNIT-CTLNETWORK-06 | `CTL-12` | A key stored, and a state folder that others can read | Join | The key file and the state folder are for the owner only. The identity is in the state folder |
| UNIT-CTLNETWORK-07 | `CTL-12` | A key stored that the stand-in refuses | Join | The error about a refused key. It says to run `login` and does not hold the key |
| UNIT-CTLNETWORK-08 | `CTL-12` | A key stored. Tailscale's servers never answer | Join | An error after the timeout. It names the timeout and does not hold the key |
| UNIT-CTLNETWORK-09 | `CTL-12` | A key stored. A file stands where the state folder must go | Join | An error that does not hold the key. No device joined |
