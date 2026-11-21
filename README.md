# Gangway

Remote access for AI, with just-in-time approval of commands.

An AI tool such as Claude Code asks to run a command or read a file on a remote machine. A person at that machine sees the request and says yes or no. Nothing runs without a yes.

It exists because SSH gives an AI a full shell with no say over the commands.

## Status

The first version, the MVP, is being built piece by piece. It cannot be run yet.

The design is written down in three levels, each with its tests: the requirements, the architecture, and the inside of each piece. The pieces are built one at a time, and every test in the code names the requirement it proves.

## Folders

| Folder | What is in it |
|---|---|
| `cli/` | The cli-tool, which runs on the host and holds the gate. Go |
| `controller/` | The controller, which the AI calls. Go |
| `shared/` | The Go code both programs use |
| `acceptance/` | The acceptance tests of `requirements.md`. They start both real programs. Go |
| `skill/` | The skill that teaches the AI how to ask |

## Run the tests

```
cd cli && go test ./...
```

The same goes for each folder that holds Go code. The tests also run on GitHub for every pull request.

## Read next

- `blueprint.md`: what it is and how the parts fit
- `plan.md`: the steps, the MVP, and how each step is checked
- `requirements.md`: the hard requirements, each with its acceptance tests
- `architecture.md`: the API, the messages, and the pieces of each program
- `design.md`: the inside of each piece, with its unit tests
- `diagrams.md`: the design as diagrams
- `ux.md`: what the user sees
- `CLAUDE.md`: the decisions and why
