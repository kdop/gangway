---
name: gangway
description: Run commands on a remote host through Gangway, where a person at the host approves each command. Use when the user pastes a line that starts with "/gangway", or a line that holds a controller address and a key that starts with "gw_".
---

# Gangway

Gangway lets you run commands on a remote host. A person sits at that host. Each command you ask for is shown to them, and it runs only if they press `y`. You never get a shell: you ask for one command at a time, and you get back what it wrote.

The user starts a session on the host and pastes one line to you:

```
/gangway <controller address> <session key>
```

For example: `/gangway http://127.0.0.1:8642 gw_4f9c…`

## Start

1. Take the address and the key from the line. The key must start with `gw_`. If it does not, stop and tell the user the line is not a Gangway line.
2. Keep the key out of your replies. Never print it, and never put it in a file the user keeps.
3. Put the address and the key in shell variables, in every command that calls the controller:

   ```
   GW=http://127.0.0.1:8642
   KEY=gw_4f9c…
   ```

4. Say hello, once. This checks the key, makes you the owner of the session, and tells you the limits:

   ```
   curl -sS -X POST "$GW/v1/hello" -H "Authorization: Bearer $KEY"
   ```

   The answer holds a `caller_token`, the host's name, the limits, and `seconds_left`: how long the session still lasts. Only the person at the host can make it longer.

5. Keep the caller token like the key: out of your replies and out of files. Put it in a third shell variable, and send it with every later call as a second header:

   ```
   CALLER=gwc_81b2…
   ```

   **`hello` works only once.** A second `hello` gets a 401, and so does every call without the token. If you lose the token, the session is lost to you: tell the user, who can start a new session and paste a new line.

## Ask for a command

Write the command to a file first, so that quotes and line breaks arrive as they are. Then send it:

```
cat > /tmp/gw_cmd.txt <<'GW_END'
grep ERROR /var/log/app.log | tail -n 20
GW_END
jq -Rs '{kind: "command", command: (. | rtrimstr("\n"))}' /tmp/gw_cmd.txt |
  curl -sS --max-time 600 -X POST "$GW/v1/requests" \
    -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER" \
    -H "Content-Type: application/json" -d @-
```

**Give the call ten minutes.** The call stays open while the person reads the command and while the command runs. Use `--max-time 600`, and set the time limit of your own shell tool to ten minutes (in Claude Code: `timeout: 600000`). If your call is cut short, Gangway takes it as "the AI is gone" and kills the command.

Send one request at a time and wait for its answer.

## The answers

Every answer is JSON with a `state`.

### `refused`: nothing ran

```json
{ "state": "refused", "reason": "user" }
```

| `reason` | What it means | What to do |
|---|---|---|
| `user` | The person said no | Do not send the same command again. Tell the user what you wanted and why, and ask how to go on |
| `control_character` | The command holds a character that is not allowed | Fix the command, see "What a command may hold" |
| `line_full` | Too many of your requests wait | Wait for the ones you sent |
| `time_up` | The session's time is up. The person at the host is asked whether to extend it | Do not send it again at once. Tell the user. If they extend, ask again. If they do not, the session ends |
| `session_ended` | The session is over | Stop. Tell the user. They can start a new session and paste a new line |

### `ended`: the command ran and is over

```json
{ "state": "ended", "how": "exit", "exit_code": 0, "output": "…", "cut": false, "bytes_replaced": false }
```

| `how` | What it means |
|---|---|
| `exit` | It ended by itself. Look at `exit_code` |
| `killed_by_user` | The person stopped it with Ctrl+C. Take that as a no: ask before you try again |
| `killed_by_ai` | You said stop |
| `killed_no_answer` | You did not answer "wait or stop" in time |
| `killed_time_up` | The session's time was up, so the command got no more time at its time limit. You were not asked |
| `start_failed` | It could not be started. `output` says why |

- `output` holds normal and error output together, in order.
- `bytes_replaced: true` means the output held bytes that are not text, and each was replaced by `�`. To get such data whole, ask for it as text: `base64 file`.
- `cut: true` means the output was too long and only the first part came.

### `running`: the time limit passed and the command is not over

```json
{ "state": "running", "id": "r_81c2", "output": "…", "answer_within_seconds": 60 }
```

You must now answer, within that many seconds, or the command is killed:

```
curl -sS --max-time 600 -X POST "$GW/v1/requests/r_81c2/wait" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER"
curl -sS -X POST "$GW/v1/requests/r_81c2/stop" \
  -H "Authorization: Bearer $KEY" -H "Gangway-Caller: $CALLER"
```

- `wait` lets it run on. The call stays open like the first one and gives the same kinds of answer. Its `output` holds only what is new.
- `stop` kills it and gives `ended`.
- Choose `wait` only when you expect the command to end. For a command that never ends by itself, choose `stop`.

### When the call itself is turned away

| HTTP status | What it means |
|---|---|
| 401 | The key is wrong, the caller token is missing or wrong, or the session is over. Check that you sent both headers. If you did, stop and tell the user |
| 404 | That `id` is not a running command. It has ended |

## What a command may hold

- Any text, with line breaks and tabs. Pipes, `>`, `;` and `&&` work: the line runs through `/bin/sh`.
- No control characters. Write a code in its written form: `printf '\033[0m'`, never the raw character.
- No invisible characters and no characters that change the reading order.

## How to ask well

The person reads every command before it runs. Make that easy.

- **One clear step per command.** Short commands get a yes faster than long ones.
- **Ask for little.** Use `head`, `tail`, `grep`, `wc -l`. Do not print whole large files.
- **No keyboard.** A command cannot ask a question. Use the forms that need no answer: `apt-get -y`, `rm -f`, `git --no-pager`, `top -b -n 1`, `systemctl --no-pager`, `journalctl --no-pager -n 50`.
- **No screen programs.** No `vim`, `less`, `htop`, `watch`. Read with `cat`, `head` or `sed -n`. Change a file with `sed -i`, or write it whole with a here-document.
- **Nothing that runs forever.** Not `tail -f`: use `tail -n 50`. Not `ping host`: use `ping -c 3 host`.
- **A program that must keep running** needs `nohup`, `&`, and its output in a file: `nohup ./app > app.log 2>&1 &`. Without the file it dies when the command ends.
- **No `sudo`** unless the user told you it works with no password on this host. It cannot ask for one.
- **Say what you are about to do** to the user in your reply before a command that changes something, so the prompt on the host is no surprise.

## When things go wrong

- The call hangs for long: the person has not answered yet. Wait for the ten minutes. Do not send the command a second time.
- A `refused` with `user`, or a `killed_by_user`: the person disagrees. Ask, do not push.
- A 401 in the middle of the work: the session is over. Nothing more can run until the user pastes a new line.
