# Gangway UX

How the tool looks and feels to the user. The design is in `blueprint.md`. The steps are in `plan.md`.

## cli-tool

- **The paste line is a command.** The cli-tool prints it in this form:

  ```
  /gangway https://controller.example gw_4f9c…
  ```

  In Claude Code a line that starts with `/` and a skill's name runs that skill at once, so the AI does not have to guess. In a tool with no such commands, the word "gangway" still tells the AI which skill to use. The `gw_` at the front of the key marks it as a Gangway key: the skill can check it, and tools that scan for leaked secrets can spot it.
- **One key answers the prompt.** Lower-case `y` or lower-case `n`, with no Enter. Every other key is ignored, capitals too. A key pressed in the first half second after a prompt shows is ignored too, so a key meant for the last prompt cannot answer the new one.
- **Ctrl+C stops the nearest thing.** One Ctrl+C cancels what is in front: it kills the running command, or refuses the request at a waiting prompt. The session lives on. With nothing in front, Ctrl+C ends the session. Two Ctrl+C close together always end the cli-tool, even if the AI sends a new request in between.
- **A session has a length, and says so.** At start the cli-tool says how long the session lasts. The length is `SESSION_LENGTH`, 30 minutes to start with. Another length can be asked for at start, for this one session: `gangway --length 45m`. The time is a number with a unit, such as `90s`, `45m` or `2h`.
- **When the time is up, the cli-tool asks whether to extend.** The question takes the same keys as a prompt: lower-case `y` or `n`, with no Enter, and no key in the first half second. Ctrl+C means no. `y` gives the same length again. `n`, or no answer within `EXTEND_WAIT`, ends the session.
- **A host can have a maximum.** Root sets `SESSION_MAX` in `/etc/gangway/settings`, as `SESSION_MAX = 2h`. No session on that host lasts longer, extensions included. A length above it is cut to it, and the cli-tool says so at start. At the maximum the session ends with no question. A file there that is not root's, that others can write, or that holds a mistake stops the start, with the reason.
- **Every session is logged, and the cli-tool says where.** The log is a text file in a new folder under the temp folder, such as `/tmp/gangway-2780822553/session.txt`. It holds each request, its answer and the output, with the time. Only the user can read it. The path is shown when the session starts and again as the last line when it ends. The cli-tool does not delete it. It lasts until the system empties the temp folder, on many hosts at every start.
- **Dots while it connects.** On start the cli-tool prints dots until it has joined Tailscale and reached the controller. They show it is working and not stuck.

## What the terminal shows

Lines of the cli-tool start at the start of a line. Lines that come from a request or a command are set in, each kind with a mark of its own.

A request and its answer:

```

AI asks to run:
  > grep ERROR app.log | tail -n 20
Run it? y/n y
  | 2026-11-21 ERROR disk full
  | 2026-11-21 ERROR disk full
```

- `  > ` starts each line of the command the AI asked for.
- `  | ` starts each line the command wrote.
- A character that could move the cursor, or that cannot be seen, is shown as `�`.

How a command ended. A command that ended by itself gets no line: its output is the last thing shown.

| Line | When |
|---|---|
| `Killed: you pressed Ctrl+C.` | |
| `Killed: the AI said stop.` | |
| `Killed: the time limit passed and the AI gave no answer.` | |
| `Killed: the time limit passed and the session's time was up.` | The command ran when the session's time ran out. It was left to run to its time limit |
| `Killed: the AI hung up.` | |
| `Killed: the session ended.` | |
| `The command could not be started.` | The shell is not there |

Other lines:

| Line | When |
|---|---|
| `The AI hung up. Nothing ran.` | The AI left while the prompt waited |
| `Refused a request with no prompt: it holds a character that is not allowed.` | |
| `Refused a request with no prompt: too many requests wait.` | |
| `Refused a request with no prompt: the session's time is up.` | The AI asked while the question to extend waited, or while the last command still ran |
| `The session's time is up. Nothing ran.` | A prompt waited when the time ran out |
| `The session's time is up. The command that runs gets no more time.` | A command ran when the time ran out. The question to extend comes when it is over |

When the session's time is up:

```

The session's time is up.
Extend it by 30 minutes? y/n y
The session goes on for 30 minutes.
```

After `n` the next line is `The session was not extended: the session ends.` With no answer in time, the line `No answer in time.` comes first.

On a host with a maximum, the start says so. A length above it:

```
1 hour is above the maximum of 20 minutes, set in /etc/gangway/settings. It was cut to 20 minutes.
The session lasts 20 minutes. That is the maximum: then it ends.
```

A length below it:

```
The session lasts 15 minutes. Then you are asked whether to extend it, up to the maximum of 20 minutes.
```

When less than a full stretch is left to the maximum, the question names what is left:

```

The session's time is up.
Extend it by 5 minutes, up to the maximum? y/n y
The session goes on for 298 seconds. That reaches the maximum: then it ends.
```

At the maximum there is no question. The line is `The session has lasted 20 minutes, the maximum: the session ends.`

The log. At the start, under the line that tells the length:

```
The session is logged at /tmp/gangway-2780822553/session.txt
```

At the end, as the last lines:

```
The session is over. Its key is dead.
Session logged at /tmp/gangway-2780822553/session.txt
```

| Line | When |
|---|---|
| `That file holds the last part. The parts before it are in the same folder.` | Under `Session logged at`, when the log grew past `LOG_FILE_SIZE` and has more than one file |
| `gangway: cannot make the log of the session in /tmp: …` | At start. No session starts without a log |
| `The log cannot be written: the session ends.` | The log failed in the middle of a session |
| `The log is not whole: it could not be written. What there is of it is at …` | In place of `Session logged at`, after such a failure |
