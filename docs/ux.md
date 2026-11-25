# Gangway UX

How the tool looks and feels to the user. The design is in `blueprint.md`. The steps are in `plan.md`.

## cli-tool

- **The paste line is a command.** The cli-tool prints it in this form:

  ```
  /gangway https://controller.example gw_4f9c…
  ```

  In Claude Code a line that starts with `/` and a skill's name runs that skill at once, so the AI does not have to guess. In a tool with no such commands, the word "gangway" still tells the AI which skill to use. The `gw_` at the front of the key marks it as a Gangway key: the skill can check it, and tools that scan for leaked secrets can spot it.
- **The paste line starts at the start of its line.** Every other line of the cli-tool is set in. A copy of the whole line then starts with the slash. With spaces in front, an AI tool does not take it as a command.
- **One key answers the prompt.** Lower-case `y` or lower-case `n`, with no Enter. Every other key is ignored, capitals too. A key pressed in the first half second after a prompt shows is ignored too, so a key meant for the last prompt cannot answer the new one. The key itself is not shown: the next line says what happened.
- **Ctrl+C stops the nearest thing.** One Ctrl+C cancels what is in front: it kills the running command, or refuses the request at a waiting prompt. The session lives on. With nothing in front, Ctrl+C ends the session. Two Ctrl+C close together always end the cli-tool, even if the AI sends a new request in between.
- **A session has a length, and says so.** At start the cli-tool says how long the session lasts. The length is `SESSION_LENGTH`, 30 minutes to start with. Another length can be asked for at start, for this one session: `gangway --length 45m`. The time is a number with a unit, such as `90s`, `45m` or `2h`.
- **A prompt says how much time is left.** The header of each prompt names the time the session still takes requests, as it was when the header was written. It is not a clock: a header on screen does not change.
- **When the time is up, the cli-tool asks whether to extend.** The question takes the same keys as a prompt: lower-case `y` or `n`, with no Enter, and no key in the first half second. Ctrl+C means no. `y` gives the same length again. `n`, or no answer within `EXTEND_WAIT`, ends the session.
- **A host can have a maximum.** Root sets `SESSION_MAX` in `/etc/gangway/settings`, as `SESSION_MAX = 2h`. No session on that host lasts longer, extensions included. A length above it is cut to it, and the cli-tool says so at start. At the maximum the session ends with no question. A file there that is not root's, that others can write, or that holds a mistake stops the start, with the reason.
- **Every session is logged, and the cli-tool says where.** The log is a text file in a new folder under the temp folder, such as `/tmp/gangway-2780822553/session.txt`. It holds each request, its answer and the output, with the time. Only the user can read it. The path is shown when the session starts and again when it ends. The cli-tool does not delete it. It lasts until the system empties the temp folder, on many hosts at every start.
- **The end sums the session up.** Why it ended, how long it lasted, how many requests ran and how many were refused, and where the log is.
- **Nothing on screen is ever changed.** The cli-tool only writes on, line after line. It never moves the cursor up and never clears anything, so what scrolled by is a true record of the session.

## The look

- **Signs and colors are decoration.** Every line says the same with them and without them. Nothing is told by a color alone.
- **A plain terminal gets a plain look.** With `NO_COLOR` set the cli-tool writes no color. When `TERM` is `dumb`, or the locale is not UTF-8, it writes plain ASCII: `-` for `─`, `|` for `│`, `+` for `✓`, `x` for `✗`.
- **The banner is the name only.** It is shown once, at the start, and left out on a terminal that is too narrow for it.
- **Times are short.** `30 min`, `2 h`, `1 min 30 s`. A time that runs, such as the time left, is rounded down.

## What the terminal shows

Lines of the cli-tool are set in by two spaces. Lines that come from a request or a command carry a mark at the same place, each kind its own. No line of the cli-tool starts with such a mark, so a command, or what it prints, cannot pass for a line of the cli-tool.

- `  > ` starts each line of the command the AI asked for.
- `  │ ` starts each line the command wrote.
- A character that could move the cursor, or that cannot be seen, is shown as `�`.
- `✓` starts the line of something that went through. `✗` starts the line of something that did not: refused, killed, not extended.

### The start

```
   ____
  / ___| __ _ _ __   __ ___      ____ _ _   _
 | |  _ / _` | '_ \ / _` \ \ /\ / / _` | | | |
 | |_| | (_| | | | | (_| |\ V  V / (_| | |_| |
  \____|\__,_|_| |_|\__, | \_/\_/ \__,_|\__, |
                    |___/               |___/

  Joining Tailscale ...

  host      build-box
  session   30 min, then you are asked to extend
  network   connected over Tailscale
  log       /tmp/gangway-2780822553/session.txt

  Paste this line to the AI:

/gangway https://controller.example gw_4f9c…

  y run   n refuse   Ctrl+C end
```

In the one-machine mode the line under the banner is `Connecting to` and the address, and the network row is `connected, on this machine only`.

On a host with a maximum, the session row says so. A length below it:

```
  session   15 min, then you are asked to extend, up to the maximum of 20 min
```

A length above it:

```
  session   20 min, the maximum: then it ends
            1 h is above the maximum: it was cut to 20 min
            the maximum is set in /etc/gangway/settings
```

### A request

```

  ── request 3 ──────────────────────────  27 min left

  > grep ERROR app.log | tail -n 20

  run it?   y yes   n no 
```

The number counts the prompts of the session. After `y`:

```

  ── request 3 ── running ───────────────  Ctrl+C stops it

  │ 14:02:11 ERROR disk full
  │ 14:02:15 ERROR disk full

  ✓ ended, exit 0, 2 lines sent to the AI
```

A command that ended by itself always gets the `✓` line, with its exit code, whatever the code is. With no output the line ends in `no output`.

After `n` or Ctrl+C, right under the question:

```
  ✗ refused, nothing ran
```

How a prompt can close with nothing run:

| Line | When |
|---|---|
| `✗ refused, nothing ran` | `n` or Ctrl+C |
| `✗ the AI hung up, nothing ran` | The AI left while the prompt waited |
| `✗ the session's time is up, nothing ran` | A prompt waited when the time ran out |
| `✗ the session ended, nothing ran` | A prompt waited when the session ended |

How a command can end:

| Line | When |
|---|---|
| `✓ ended, exit 0, 2 lines sent to the AI` | It ended by itself |
| `✗ killed: you pressed Ctrl+C` | |
| `✗ killed: the AI said stop` | |
| `✗ killed: the time limit passed and the AI gave no answer` | |
| `✗ killed: the time limit passed and the session's time was up` | The command ran when the session's time ran out. It was left to run to its time limit |
| `✗ killed: the AI hung up` | |
| `✗ killed: the session ended` | |
| `✗ the command could not be started` | The shell is not there |

A request that gets no prompt:

| Line | When |
|---|---|
| `✗ refused a request with no prompt: it holds a character that is not allowed` | |
| `✗ refused a request with no prompt: too many requests wait` | |
| `✗ refused a request with no prompt: the session's time is up` | The AI asked while the question to extend waited, or while the last command still ran |

When such a line comes while a prompt or the question to extend waits, the prompt or the question is shown again under it, so it is always the last thing on screen.

### When the session's time is up

```

  ── time is up ─────────────────────────  answer in 60 s

  extend by 30 min?   y yes   n no 
  ✓ extended by 30 min
```

After `n` or Ctrl+C the line is `✗ not extended`. With no answer in time it is `✗ no answer in time, not extended`. Then the session ends.

A command that runs when the time runs out is not stopped. The line is `The session's time is up. The command that runs gets no more time.` The question comes when the command is over.

When less than a full stretch is left to the maximum, the question names what is left, and does not wait past the maximum:

```

  ── time is up ─────────────────────────  answer in 60 s

  extend by 5 min, up to the maximum?   y yes   n no 
  ✓ extended by 4 min 58 s, up to the maximum: then the session ends
```

At the maximum there is no question. The session ends.

### The end

```

  ── session over ───────────────────────

  reason    Ctrl+C
  duration  42 min
  requests  7 run, 1 refused
  log       /tmp/gangway-2780822553/session.txt
```

A request counts as refused whoever refused it: the user, the cli-tool, or the end of the session. With no request the row says `none`.

The `reason` row holds one of a fixed set of short texts:

| Reason | When |
|---|---|
| `Ctrl+C` | The user pressed Ctrl+C, once or twice |
| `Expired` | The time was up and the session was not extended, or it reached the maximum |
| `Controller ended it` | The controller ended the session |
| `Connection lost` | The connection to the controller was lost |
| `Log failed` | The log could not be written |
| `Stopped from outside` | The program was told to stop from outside |
| `Terminal closed` | The terminal is gone |
| `Unknown` | Anything else |

When a command still runs as the session ends, one line comes first, and the end is shown when the command is gone:

```
  The session ends: you pressed Ctrl+C twice. The command is being stopped.
```

The log:

| Line | When |
|---|---|
| `That file holds the last part. The parts before it are in the same folder.` | Under the log row, when the log grew past `LOG_FILE_SIZE` and has more than one file |
| `The log is not whole: it could not be written.` | Under the log row, after the log failed in the middle of a session |
| `gangway: cannot make the log of the session in /tmp: …` | At start. No session starts without a log |

### Before a session

A start that fails says why in one line that starts with `gangway:`, and ends. No banner comes before a mistake in the settings or in the admin file.

`gangway login` shows the banner, then:

```
  Paste the Tailscale auth key and press Enter. It is not shown: 
  Trying the key on Tailscale ...
  This host is ready. Start a session with: gangway
```
