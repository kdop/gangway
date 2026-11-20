# Gangway blueprint

Gangway: remote access for AI, with just-in-time approval of commands.

High level only. Steps and checks are in `plan.md`. Reasons for each decision are in `CLAUDE.md`.

## The problem

I debug remote machines that have no AI on them. Today the AI tells me what to type and I copy and paste, both ways. I want the AI to read files and run commands on the host itself.

SSH would do that, but SSH hands over a shell. I have no say over what runs in it. I will not give an AI that.

## What Gangway does

The AI asks. A person at the host says yes or no. Only then does the command run.

- Default is no. Nothing runs without a yes.
- Access is short. A session ends on Ctrl+C or when its time is up: 30 minutes unless I set another length.
- Hosts are Linux only.

## Parts

| Part | Runs on | Job |
|---|---|---|
| **cli-tool** | The host | Starts a session, shows each request, asks for approval, runs what is approved, sends back the result |
| **controller** | A machine of mine on Tailscale | A web app: a REST API and a dashboard. Passes requests from the AI to the cli-tool and results back. Runs nothing itself |
| **skill** | The AI tool | Teaches the AI how to call the controller with the session key |

## How they talk

```
AI tool --(API + session key)--> controller --(Tailscale)--> cli-tool on the host
```

- The cli-tool dials out to the controller over Tailscale. The host listens on nothing.
- Tailscale is built into the cli-tool. The host needs no Tailscale install and no root.
- The AI calls the controller's API. The AI machine must reach the address the controller listens on: the same machine by default, or Tailscale when set so.

## A session

1. I start the cli-tool on the host. It joins my Tailscale network by itself and connects to the controller. It prints dots while it connects.
2. The cli-tool makes a session key and shows one line to paste. The line holds the controller's API address and the key.
3. I paste the line to the AI. The skill knows what to do with it.
4. The AI asks to run a command or read a file. The controller passes the request to the cli-tool.
5. The cli-tool shows the request in its terminal. I press `y` (yes) or `n` (no).
6. On yes, the cli-tool runs it as my plain user and sends back the result. On no, the AI is told it was refused.
7. The session ends when I press Ctrl+C or when its time is up and I do not extend it. The key is dead after that, and the host drops off my Tailscale network.

Every session is new. No resume.

## The controller

- A web app with two faces: a REST API for the AI, and a dashboard for me.
- The dashboard shows the live sessions and the log of requests and answers, and has a button to end a session.
- The dashboard does not approve commands in the first version. Approval stays on the host.
- Where it listens is a setting, not a design matter. By default it answers on localhost only. It can be set to answer on Tailscale.
- It has no login of its own in the first version. So it must not be set to answer on a public address.
- Built in Go, as one program. Pages are made on the server with Go's own templates, plus htmx for live updates. No Vue or React, and no build step, until the dashboard grows enough to need them.

## Joining Tailscale

- The cli-tool is written in Go and uses `tsnet`, Tailscale's library that puts Tailscale inside a Go program. It needs no root and no system service.
- It joins with a Tailscale auth key, not a browser login. I make the key in the Tailscale admin page.
- I run `login` once after install and paste the key. After that, starting a session asks for nothing. The key lasts 90 days at most, then I run `login` again.
- The key is kept in a file in my home folder that only my user can read. The exact place follows the usual practice for such files and is picked when it is built.
- The cli-tool never gives that file to the AI. The ban is fixed in the code, not a setting. Limit: this is sure for file reads. An approved command could still print the file, so I must refuse such a command.
- The key is ephemeral: Tailscale removes the host from my network after it goes offline.
- The key carries a tag. My Tailscale rules let that tag reach the controller and nothing else.
- Later: the controller makes a fresh one-off auth key per session through the Tailscale API.

Sources: https://tailscale.com/kb/1244/tsnet, https://pkg.go.dev/tailscale.com/tsnet, https://tailscale.com/kb/1085/auth-keys

## The gate

- The gate lives in the cli-tool, on the host. The host is what gets hurt, so the host has the last word.
- Reading a file is a request like any other. It passes the gate too.
- In the first version every request is asked, each time. "Always allow" comes in the second stage.
- The gate is never in the AI, and in the first version never in the controller. If the yes came from the controller, the cli-tool could not tell a real yes from a fake one.
- Later, as a choice I switch on per session: approve from the dashboard. Good for many hosts or a phone. It means trusting the controller. The cli-tool keeps its hard limits either way.

## Limits on a command

For safety, so nothing hangs forever. Not meant to be strict.

- **Time.** When a command passes the time limit, the AI is told it is still running and is asked: wait more, or stop?
- **Size.** When the output passes the size limit, the AI gets the first part and is asked: keep receiving, or stop? A small limit is good: it pushes the AI to ask for less, with `head`, `tail` or a sharper command.
- The question goes to the AI through the controller. The cli-tool does the stopping, since it holds the running command.
- Both limits are settings. Starting values: 60 seconds and 100 KB.
- Ending the session stops any running command.

## Folder grants (later stage)

Not in the first version. Until then every file read is asked like a command.

- When I start the cli-tool I name the folders the AI may read, for example `--allow /var/log --allow ~/app`.
- A file inside a granted folder is read without a prompt. Naming the folder was the yes.
- A file outside every granted folder is refused, with no prompt.
- The cli-tool checks the real path, so a link that points outside a granted folder is refused.
- Limit: this holds for file reads only. A command can touch any path my user can. Commands are still approved one by one, and they start in a granted folder. Locking a command inside a folder needs a sandbox, which comes later still.

## Always allow (second stage)

Not in the first version. Ideas so far, nothing fixed:

- The prompt gets more answers: yes, no, always allow.
- Always allow for the exact command is weak: the AI may never ask the same command again.
- So also: always allow the program, for example "always allow `ls`".
- And: a list of safe, read-only commands that can be allowed as a group.
- The choice can hold for one session, or be saved for later sessions.
- An allow rule is a shortcut, not the gate. It holds what I already said yes to.

Traps to handle when this is built:

- A command allowed by its program name must run with no shell. Otherwise `ls; rm -rf x` passes as `ls`.
- Programs that can run other programs or write files stay off any safe list, for example `find` (it has `-exec`).
- Read-only is not the same as harmless. `cat` on a key file leaks the key.

## What is stored

No database in the first version. Plain files only.

On the host:

| What | Where |
|---|---|
| Settings: controller address, time and size limits | `~/.config/gangway/` |
| Tailscale auth key from `login` | Same folder, own file, readable by my user only |
| Log of requests and answers | `~/.local/state/gangway/` |
| Saved allow rules (second stage) | `~/.config/gangway/` |

On the controller:

- Live sessions are kept in memory only. If the controller restarts, every session ends and I start new ones.
- Its settings, such as where it listens, are a small file or start options.
- The dashboard shows what happened since the controller started. The full record is the log on each host. History that survives a restart comes later, in one SQLite file.

Sessions are never stored. Nothing is left on disk that could be picked up and used again.

## The session key

- Made by the cli-tool, long and random, easy to copy.
- Lives as long as the session: 30 minutes by default. See "Session length".
- Belongs to the first AI that uses it. A second user of the same key is refused.
- It gives the right to ask, not the right to run. A stolen key gets a stranger a prompt that I refuse.

## Session length

- The default is 30 minutes. It is a setting on the host.
- I can pick another length when I start a session.
- When the time is up, the cli-tool asks me in its terminal whether to extend. No answer within a short time means no, and the session ends.
- There is a maximum, set at admin level. It caps the whole session, extensions included. At the maximum the session ends with no question.
- Only the person at the host terminal can change the length. The AI and the controller can never make a session longer.

## Security

This is remote command running by design. The aim is a door that is narrow, in plain sight and short-lived.

| Risk | Answer |
|---|---|
| Stolen session key | It only lets someone ask. It dies with the session. It is tied to the first AI |
| Controller taken over | It can only ask. The gate on the host still decides |
| AI asks for something harmful | I see the exact command before it runs and say no |
| I approve without reading | Not solved by code. Short sessions and a log limit the harm |
| Tool runs with too much power | Runs as a plain user. Refuses to start as root |
| Host stays on my network | It joins as an ephemeral, tagged machine. It can reach only the controller and is removed after it goes offline |
| Left running by mistake | Runs only in an open terminal, never in the background. Ends when its time is up unless I extend it at the terminal, and never runs past the maximum |

Every request and its answer is written to a log.

## Not in the first version

- Web page and Google login
- Resuming a session
- sudo
- More than one user
- Approving from the dashboard or another device
- Dashboard history that survives a restart
- Always allow, and lists of safe commands
- A fresh Tailscale auth key per session, made by the controller
- Folder grants, and a sandbox that locks commands inside the granted folders

## Open questions

- Where the maximum session length is set. "Admin level" could be the controller, since it sees every session.
