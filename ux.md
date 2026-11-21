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
- **Dots while it connects.** On start the cli-tool prints dots until it has joined Tailscale and reached the controller. They show it is working and not stuck.
