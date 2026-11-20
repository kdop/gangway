# CLAUDE.md — Gangway project instructions

Voice: `.claude/rules/voice.md`. Rules: `.claude/rules/`.
Git work: the `git` skill.

Talk to the user in laconic caveman English.

## Notes

Continuity between sessions comes from the project notes: `notes/<yyyy-mm>.md`, one file
per month, appended at the bottom. If it is not in the notes or the docs, it is gone
next session.

## Project context

Gangway is just-in-time approval of the commands an AI may run on a host. `blueprint.md`
says what it is. This file says how it is built and why.

### Files

| File | Purpose |
|---|---|
| `README.md` | What Gangway is, in a few lines, and the status |
| `blueprint.md` | The design: parts, session flow, gate, key, security, open questions |
| `plan.md` | The steps, in order, each with its check |
| `notes/<yyyy-mm>.md` | The timeline, one file per month |
| `.claude/` | Rules (`rules/`, with the voice in `rules/voice.md`) and skills (`skills/`) |

### Decisions and why

Each line is dated. "Kostas" means he said it. "Claude" means Claude made the call and
Kostas can overturn it.

- **No SSH for the AI** (2026-11-19, Kostas). SSH hands over a shell with no say over the commands. That say is the whole point of Gangway.
- **Not a product** (2026-11-19, Kostas). One user. No sign-up, no web page, no accounts in the first version.
- **Three parts: cli-tool, controller, skill** (2026-11-19, Kostas). The host has no AI, so something must sit on the host, something must reach it, and the AI must know how to ask.
- **Host to controller goes over Tailscale; the AI calls the controller's API** (2026-11-19, Kostas). The host is never open to the internet. The AI machine does not have to be on Tailscale.
- **The cli-tool is written in Go** (2026-11-19, Kostas). `tsnet` is Go only, and Go gives one file to copy to the host with nothing else to install.
- **Tailscale is built into the cli-tool with `tsnet`** (2026-11-19, Claude; Kostas asked for joining to be part of the flow). The host needs no Tailscale install and no root. Source: https://pkg.go.dev/tailscale.com/tsnet ("No root privileges required", "No system daemons to install or manage").
- **The host joins with an ephemeral, tagged auth key** (2026-11-19, Claude). It leaves the network after it goes offline, and the tag limits it to the controller. Source: https://tailscale.com/kb/1085/auth-keys.
- **Linux hosts only** (2026-11-19, Kostas). Keep it simple.
- **`login` once, then start with no questions** (2026-11-19, Kostas). The auth key is pasted at `login`, about once a month, and kept in a file in the home folder that only the user can read.
- **The cli-tool never gives its own key file to the AI; the ban is fixed in code** (2026-11-19, Kostas). A setting could be switched off by mistake.
- **A command has a time limit and a size limit, and neither is strict** (2026-11-19, Kostas). They stop a hang. A small size limit also pushes the AI to ask for less.
- **Past a limit the AI is asked whether to go on; the cli-tool does the stopping** (2026-11-19, Claude; Kostas leaned to the controller as the place to ask). The command is already approved, so going on is not a safety call, and only the cli-tool holds the running command.
- **No database in the first version; plain files in the home folder** (2026-11-19, Claude; Kostas agreed). Settings and the key in `~/.config/gangway/`, the log in `~/.local/state/gangway/`. Those are the usual Linux places.
- **Sessions are never stored, on either side** (2026-11-19, Kostas: no resume; Claude: memory only on the controller). A controller restart ends every session, which is the safe way to fail.
- **The controller is a web app: a REST API with a dashboard** (2026-11-19, Kostas).
- **The controller is written in Go too, not Django, FastAPI, Vue or React** (2026-11-19, Claude; Kostas left the choice open and is tired of Django). One language, both sides share the same message types, and it ships as one file. Go's templates with htmx are enough for a page that lists sessions and a log.
- **Where the controller listens is a setting; the default is localhost** (2026-11-19, Kostas). It is a deployment matter, not a design one.
- **The controller has no login in the first version, so it must not listen on a public address** (2026-11-19, Claude). Localhost and Tailscale are both closed to strangers.
- **The dashboard does not approve commands** (2026-11-19, Claude). The gate stays on the host.
- **The gate is never in the AI** (2026-11-19, Kostas). The thing being checked cannot be the checker.
- **The session key lives 30 minutes by default** (2026-11-19, Kostas). Access ends by itself.
- **The cli-tool grants access to named folders, at a later stage** (2026-11-19, Kostas). The AI should see only what the job needs. Not in the first version.
- **An allow rule by program name runs with no shell** (2026-11-19, Claude). With a shell, `ls; rm -rf x` would pass as `ls`.
- **Folder grants are hard for file reads only** (2026-11-19, Claude). A command can touch any path the user can; holding it inside a folder needs a sandbox, which is left for later.
- **The gate lives in the cli-tool, on the host** (2026-11-19, Claude; Kostas asked cli-tool or controller and accepted this). The host is what gets hurt. A controller that is taken over can then only ask. A yes sent by the controller could be faked.
- **Approval from the dashboard is later, and switched on per session** (2026-11-19, Claude). It is comfort bought with trust in the controller, so it must be a choice.
- **Approval is `y`/`n` in the cli-tool's terminal** (2026-11-19, Claude). That terminal is already open, so the first version needs no other screen.
- **"Always allow" is second stage** (2026-11-19, Kostas). The first version asks every time. How wide an allow rule reaches (exact command, program, safe list; session or saved) is not settled.
- **The cli-tool makes the key, and the key is tied to the first AI that uses it** (2026-11-19, Claude). A copied key is then of no use to a second caller.
- **No end-to-end signing of commands** (2026-11-19, Claude). With the gate on the host it adds little, and it costs code.
- **Plain user only, never root** (2026-11-19, Claude). It limits the harm of a wrong yes.

### Docs vs. notes

- **Docs** = durable truth: "the cli-tool only dials out *because*…" → this file
- **Notes** = the timeline: "moved the relay to websockets on 2026-11-20" → `notes/<yyyy-mm>.md`

When an architectural decision is made: document it in the docs, log when it was
implemented in the notes.

## Operational rules

1. **Check the notes first** — read the last two monthly files in `notes/` before starting work
2. **Log when a decision settles** — in this month's notes file, under the rules in `.claude/rules/notes.md`
3. **Push back only on a bad decision** — name the fact or the risk behind it. With none, do what he asks. No opinion for its own sake
4. **Push back on architecture** — when something is wrong, say so and say why
5. **Tag everything** — a `*tags: …*` line under each note heading makes `grep` land
6. **Search before asking** — `grep -rn` the notes and the code first
7. **Cite the source before concluding** — on any security or architecture call, read the code or the doc and cite the line. Notes and summaries are not sources
8. **Security is the product risk** — this tool is remote execution by design. Any change that widens what a session can do needs Kostas's go
