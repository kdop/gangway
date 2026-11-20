# Gangway UX

How the tool looks and feels to the user. The design is in `blueprint.md`. The steps are in `plan.md`.

## cli-tool

- **The paste line is a command** (2026-11-20, Kostas: start it with the skill's name; Claude: the `/` and the `gw_` mark). The cli-tool prints it in this form:

  ```
  /gangway https://controller.example gw_4f9c…
  ```

  In Claude Code a line that starts with `/` and a skill's name runs that skill at once, so the AI does not have to guess. In a tool with no such commands, the word "gangway" still tells the AI which skill to use. The `gw_` at the front of the key marks it as a Gangway key: the skill can check it, and tools that scan for leaked secrets can spot it.
- **Ctrl+C stops the nearest thing** (2026-11-20, Kostas). One Ctrl+C cancels what is in front: it kills the running command, or refuses the request at a waiting prompt. The session lives on. With nothing in front, Ctrl+C ends the session. Two Ctrl+C close together always end the cli-tool, even if the AI sends a new request in between.
- **Dots while it connects** (2026-11-19, Kostas). On start the cli-tool prints dots until it has joined Tailscale and reached the controller. They show it is working and not stuck.
