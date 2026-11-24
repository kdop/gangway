# Security

Gangway runs commands on machines, so a hole in it matters.

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
