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
