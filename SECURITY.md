# Security

Gangway runs commands on machines, so a hole in it matters.

## What keeps it safe

- **The gate is on the host, never in the AI.** The thing being checked is not the checker. A controller that is taken over can only ask.
- **Every command is shown, and needs a `y`.** You approve the exact line that runs.
- **The prompt shows what runs.** A request with a control character or an invisible character is refused before it reaches you.
- **The host is never open to the internet.** It dials out to the controller over Tailscale, which is built into the program: no root, nothing to install.
- **The session key works for the first AI only.** A copy of the key is of no use to a second caller.
- **Access ends by itself.** A session lasts 30 minutes. Only the person at the terminal can extend it, and no answer means no.
- **An admin can cap it.** Root sets the longest a session may last, in `/etc/gangway/settings`. Nothing below root can raise it.
- **Every session is logged on the host.** Each request, your answer and the output go to a text file that only you can read. No log, no session.
- **Plain user only.** The cli-tool refuses to run as root.

## Report a hole

Use the "Report a vulnerability" button on the Security tab of this repo. The report is private. Do not open a public issue for a hole.

## What counts

- A command runs without a `y` at the host's terminal.
- The prompt shows another command than the one that runs.
- A second caller can use a session.
- A session lives on after its end, or past its maximum.
- The host can be reached from outside the Tailscale network.
- A session key or a Tailscale auth key leaks.

## What to expect

One person maintains Gangway. Every report is read, but no answer time is promised.

Only the newest `main` gets fixes.
