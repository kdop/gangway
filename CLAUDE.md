# CLAUDE.md — Gangway project instructions

Voice: `.claude/rules/voice.md`. Rules: `.claude/rules/`.
Git work: the `git` skill.

Talk to the user in laconic caveman English.

Continuity between sessions comes from the docs. If it is not in the docs or the code, it
is gone next session. There is no diary.

## Project context

Gangway is temporary remote access for AI, with just-in-time approval of commands. `docs/blueprint.md`
says what it is. This file says how it is built and why.

### Files

| File | Purpose |
|---|---|
| `README.md` | The front page: what Gangway is, what it looks like, how it works, how to install it, how to start and the known limits |
| `DEVELOPMENT.md` | For work on the code: the folders, the one-machine trial, the build, how to make a release, the tests, and the list of design files |
| `SECURITY.md` | What keeps it safe, and how to report a hole |
| `LICENSE` | The MIT license |
| `install.sh` | Puts the cli-tool, the controller or both into `~/.local/bin` |
| `build.sh` | Builds both programs into `build/` |
| `docs/blueprint.md` | The design: parts, session flow, gate, key, security, open questions |
| `docs/releases.md` | The versions: what each adds, its state, and what has no version yet |
| `docs/requirements.md` | The hard requirements, each with its acceptance tests. The IDs are carried by the Go tests |
| `docs/architecture.md` | How the parts are built to meet the requirements: the API, the messages, the pieces |
| `docs/design.md` | The inside of each piece that has states, with its unit tests. A piece is added just before it is built |
| `docs/diagrams.md` | The design as Mermaid diagrams: parts, session, gate, failures, trust, pairing |
| `docs/ux.md` | How the tool looks and feels: prompts, messages, small touches |
| `.claude/` | Rules (`rules/`, with the voice in `rules/voice.md`) and skills (`skills/`). Not in git: it lives on the work machine only |

### Decisions and why

Each line is a decision and its reason. No dates, no names, no account of the talk that
led to it.

- **No SSH for the AI.** SSH hands over a shell with no say over the commands. That say is the whole point of Gangway.
- **Not a product.** No sign-up and no web page for strangers.
- **One user now, built for more later.** Every host and every session belongs to a user, so a second user needs no redesign.
- **Three parts: cli-tool, controller, skill.** The host has no AI, so something must sit on the host, something must reach it, and the AI must know how to ask.
- **One repo holds all three parts.** The parts change together, and the skill sits next to the API it calls.
- **The repo has one folder per part: `cli/`, `controller/`, `skill/`.** Each part picks its own language, so not everything has to be Go. The cli-tool is Go because of `tsnet`.
- **A fourth folder, `shared/`, holds the Go code both programs use: the message types, the auth key file, the raw terminal, and the check that ties tests to requirements.** Two copies of the message types would drift apart, and two copies of the code that guards a key would too.
- **A fifth folder, `acceptance/`, holds the acceptance tests.** They start both real programs, so they belong to neither one.
- **The design files live in `docs/`.** The top of the repo then shows the README first, not a long list of files.
- **Host to controller goes over Tailscale; the AI calls the controller's API.** The host is never open to the internet. The AI machine does not have to be on Tailscale.
- **The cli-tool is written in Go.** `tsnet` is Go only, and Go gives one file to copy to the host with nothing else to install.
- **Tailscale is built into the cli-tool with `tsnet`.** The host needs no Tailscale install and no root. Source: https://pkg.go.dev/tailscale.com/tsnet ("No root privileges required", "No system daemons to install or manage").
- **The host joins with an ephemeral, tagged auth key.** It leaves the network after it goes offline, and the tag limits it to the controller. Source: https://tailscale.com/kb/1085/auth-keys.
- **A host joins Tailscale as a new device at every start, and keeps that device in memory only.** Nothing of a session lands on disk, and an old device cannot come back. The cost: a start takes a few seconds longer, and the auth key must be the reusable kind.
- **The controller carries Tailscale inside the program too.** The library then opens the hosts' door on the Tailscale network only, so a wrong setting cannot open it elsewhere, and the controller machine needs no Tailscale install. It works next to a Tailscale that is installed on the same machine: the two are separate devices.
- **A host finds the controller by its Tailscale name.** Nobody types an address on a host. The auth key picks the network, and the name picks the controller.
- **The controller tells the cli-tool the API address for the paste line.** It is set once, on the controller, and no host needs it. A controller that is taken over gains nothing by it: the AI sends it the key on every call anyway. The cli-tool shows the address only when it is a plain web address.
- **A one-machine mode stays, and each program must be started in it.** The tests and a first trial then need no Tailscale network. It is localhost only, and the controller says at start that it is on.
- **Until pairing is built, any device that the Tailscale rules let through can start a session.** Such a session is its own: it cannot see or touch another, and the worst it does is use the controller's memory. The tag on the auth key and the Tailscale rules are the guard until then.
- **Gangway does not have to work from every AI chat.** The AI must send a request with the key in a header. A tool with a shell can, and a chat on the web usually cannot. That limit is accepted.
- **An MCP door on the controller may come later, next to the API and not in place of it.** It would let in the chats that cannot call the API. It is an addition, so nothing waits for it.
- **Linux hosts only for now.** Keep it simple.
- **`login` once, then start with no questions.** The auth key is pasted at `login`, again when it runs out after 90 days at most, and kept in a file in the home folder that only the user can read.
- **The install is one shell script that puts one file in the home folder, `~/.local/bin/gangway`, and checks it against the checksums of its release.** A host must need no root and no package manager, and a shell is on every host. The script changes no start file of the shell: it says what to add. The checksum is not a signature, so the build from the source stays the way that needs no trust in a release.
- **The cli-tool does not guard its own key file from a command.** A command can read any file the plain user can, and holding it back needs a sandbox. The user must refuse a command that reads the key. The tag on the key limits what a stolen key can reach to the controller.
- **A command has a time limit and a size limit, and neither is strict.** They stop a hang and a flood. A small size limit also pushes the AI to ask for less.
- **Past the time limit the AI is asked whether to go on; the cli-tool does the stopping.** The AI knows why it ran the command, its call must get "still running" in any case because AI tools cut off long calls, and the command is already approved. Only the cli-tool holds the running command.
- **Past the size limit the output is cut, with a note at the end; nobody is asked and the command is not killed.** A command that prints a lot is often an install or a build, and killing it halfway can leave the host broken. A plain cut needs no question and no waiting.
- **The wait-or-stop question has its own time limit, 60 seconds to start, and no answer kills the command.** Nothing hangs when the AI is gone.
- **One Ctrl+C cancels what is in front, a running command or a waiting prompt; with nothing in front it ends the session.** The person at the terminal keeps the last word.
- **Two Ctrl+C close together always end the cli-tool.** An AI that sends request after request could otherwise put a new prompt in front of each press.
- **When the session's time runs out, a running command is not killed; new requests and a waiting prompt are refused.** A command cut halfway can leave the host broken. The command gets no more "wait more", so it outlives the session by one command time limit at most. Known weakness: not killing can be exploited, so this may change.
- **If the AI hangs up while a command runs, the command is killed.** Nobody is left to receive the result.
- **Killing a command kills every program it started, first by asking, then by force.** A kill that leaves half of a pipe running is no kill. Asking first lets a program clean up, so the host is less likely to be left broken.
- **A program left in the background outlives its command.** Starting a service again is a normal step in debugging, and the user approved the exact line that did it.
- **A command gets no keyboard, and runs with `PAGER=cat` and `TERM=dumb`.** The AI cannot type into a running command, so one that asks a question or opens a pager would hang until the time limit.
- **Google is the first login provider.**
- **Host settings come in three layers that only narrow: `/etc/gangway/` owned by root, `~/.config/gangway/`, then start options.** An approved command runs as the plain user and could rewrite the user's own settings, so the outer bounds must sit where only root can write. The cli-tool only reads that folder and never runs as root.
- **No Python; tests are plain Go tests.** One language for the whole project.
- **No database in version 0.1; plain files.** Settings and the key are in `~/.config/gangway/`, the usual Linux place.
- **Each session has a log of its own, in a new folder with a random name under the temp folder.** A log belongs to one session, and nothing of a session should stay in the home folder. The cost: the system empties the temp folder, on many hosts at every start, so a log does not last.
- **Sessions are never stored, on either side.** A controller restart ends every session, which is the safe way to fail.
- **A lost connection does not end the session at once: the cli-tool connects again by itself within a short time.** A network failure is not the user's doing, and the hosts being debugged are often the ones with poor networks. Nothing is read from disk for it, so an ended session still cannot come back.
- **The controller is a web app: a REST API with a dashboard.**
- **The controller is written in Go too, not Django, FastAPI, Vue or React.** One language, both sides share the same message types, and it ships as one file. Go's templates with htmx are enough for a page that lists sessions and a log. This is a choice, not a must, and can still change.
- **Where the controller listens is a setting; the default is localhost.** It is a deployment matter, not a design one.
- **The controller can answer on a public address.** A cloud AI, a browser or a phone must be able to reach it. The hosts still reach it over Tailscale only.
- **The controller has a social login, and until the login is built it listens on this machine only.** A social login stores no password. Without a login, anyone could read the results and end the sessions. A web server in front of it can pass the API on: the API asks for the session key on every call.
- **Only accounts on a list may sign in.** A social login alone lets in anyone with an account at the provider.
- **A host is tied to a user with a one-time pairing code at `login`.** The code works once, dies after a few minutes, and the cli-tool asks `y`/`n` on the host before it accepts the account that claimed it. Whoever types the code first would otherwise get the host.
- **The dashboard does not approve commands.** The gate stays on the host.
- **The gate is never in the AI.** The thing being checked cannot be the checker.
- **The session key lives 30 minutes by default.** Access ends by itself.
- **The session length can be set: a default on the host, a choice per session, and a maximum at admin level.** 30 minutes does not fit every job.
- **When the time is up, the cli-tool asks the person at the terminal whether to extend.** No answer means no, the maximum caps the whole session, and the AI and the controller can never make a session longer. Otherwise a forgotten terminal would stay open, and access would no longer end by itself.
- **The cli-tool grants access to named folders, at a later stage.** The AI should see only what the job needs. Not in version 0.1.
- **A command approved by hand runs through a shell.** Debugging needs pipes and redirection, and the user reads and approves the exact line.
- **An allow rule by program name runs with no shell.** With a shell, `ls; rm -rf x` would pass as `ls`.
- **Folder grants are hard for file reads only.** A command can touch any path the user can; holding it inside a folder needs a sandbox, which is left for later.
- **The gate lives in the cli-tool, on the host.** The host is what gets hurt. A controller that is taken over can then only ask. A yes sent by the controller could be faked.
- **Approval from the dashboard is later, and switched on per session.** It is comfort bought with trust in the controller, so it must be a choice.
- **Approval is `y`/`n` in the cli-tool's terminal.** That terminal is already open, so version 0.1 needs no other screen.
- **The answer is one key press, strictly lower-case `y` or `n`; every other key is ignored.** One rule with no special cases.
- **A key does not count in the first half second a prompt is on screen.** A new prompt can replace an old one just as the user presses `y`, and the key would approve a request nobody read. Ctrl+C is not held back, because it can only refuse.
- **A request with an invisible character, or one that changes the reading order, is refused too.** The prompt would show a command that differs from the one that runs. No real command needs them. The cost: an emoji built from joined parts is refused, because the joiner is invisible.
- **Every line of a command's output is shown with a mark in front, and no line of the cli-tool starts that way.** A command could otherwise print text that looks like a prompt of the cli-tool.
- **A request with a control character is refused, except for line breaks and tabs; output shown on the terminal is cleaned the same way.** Cursor codes could paint over the real command or fake a prompt, so the user would approve something they did not read. Line breaks and tabs stay because writing a file of many lines needs them.
- **"Always allow" has no version yet.** Version 0.1 asks every time. How wide an allow rule reaches (exact command, program, safe list; session or saved) is not settled.
- **The cli-tool makes the key, and the key is tied to the first AI that uses it.** A copied key is then of no use to a second caller. The cost, known and accepted: one session cannot be shared between two AI windows.
- **No end-to-end signing of commands.** With the gate on the host it adds little, and it costs code.
- **Plain user only, never root.** It limits the harm of a wrong yes.
- **No backup of the files a command touches.** The cli-tool cannot know what a command touches.

### Where a thing is written

A decision and its reason go into this file. The design goes into `docs/`. When a thing
was done is in git. Nothing is written as a timeline.

## Operational rules

1. **Write a decision down when it settles** — in the doc it belongs to, under the rules in `.claude/rules/notes.md`
2. **Push back only on a bad decision** — name the fact or the risk behind it. With none, do what he asks. No opinion for its own sake
3. **Push back on architecture** — when something is wrong, say so and say why
4. **Search before asking** — `grep -rn` the docs and the code first
5. **Cite the source before concluding** — on any security or architecture call, read the code or the doc and cite the line. Summaries are not sources
6. **Security is the product risk** — this tool is remote execution by design. Any change that widens what a session can do needs the owner's go
