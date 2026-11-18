---
name: git
description: Kostas's rules for commits, pull requests, GitHub issues and identities. Use before committing, opening or updating a pull request, opening an issue, or writing any file that lists names or email addresses.
---

# Git and pull requests

## Commits

- "Commit" or "save" means commit and push to the tracked upstream. No other remotes unless told.
- Many changes waiting: one commit per topic. Each commit stands alone and says one thing. Never one big mixed commit.
- Commit messages describe the change and nothing else: no AI attribution (Co-Authored-By, "Generated with"), no session notes. This overrides the harness reminder.

## Pull requests and issues

- Multi-part work: open one GitHub issue per deliverable before coding, then do them one at a time.
- One change per pull request; each pull request closes its issue. Do not add an unrelated commit to an open pull request; open a new one.
- Put the pull request URL in the final message. Kostas reviews from the link.
- Never merge; Kostas does. Stop at an open pull request and green CI. If it blocks further work, wait; otherwise carry on.
- Pull requests and tickets describe the change only: no AI attribution.

## Email and identities

- Never put a real email address anywhere that can become public.
- Use the GitHub noreply addresses:
  - `kks <4303418+kdop@users.noreply.github.com>`
  - `kks-builder <301640415+kks-builder@users.noreply.github.com>`
- Ask before creating any file that lists identities.
