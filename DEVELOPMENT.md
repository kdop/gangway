# Working on Gangway

How to build it, try it and test it. What Gangway is and how to use it is in `README.md`.

## How the design is written

The design is written down in three levels, each with its tests: the requirements, the architecture, and the inside of each piece. The pieces are built one at a time, and every test in the code names the requirement it proves.

## Folders

| Folder | What is in it |
|---|---|
| `cli/` | The cli-tool, which runs on the host and holds the gate. Go |
| `controller/` | The controller, which the AI calls. Go |
| `shared/` | The Go code both programs use |
| `acceptance/` | The acceptance tests of `requirements.md`. They start both real programs. Go |
| `skill/` | The skill that teaches the AI how to ask |

## Try it on one machine

No Tailscale is needed for this. Use two terminals.

```
cd controller && go run . -local
cd cli && go run ./cmd/gangway --local
```

The cli-tool prints a line that starts with `/gangway`. Paste it to an AI that has the skill in `skill/gangway/`.

## Build

`./build.sh` builds both programs into `build/`: `build/gangway` and `build/gangway-controller`. Each is one file that can be copied to another Linux machine as it is.

## Run the tests

```
cd cli && go test ./...
```

The same goes for each folder that holds Go code. The tests also run on GitHub for every pull request.

To see how many lines of each package its tests run, add `-cover`: `go test -cover ./...`. On GitHub the same numbers are in the summary of each run. There is no pass mark: the check that every requirement has a test is the guard.

## Read next

- `blueprint.md`: what it is and how the parts fit
- `plan.md`: the steps, the MVP, and how each step is checked
- `requirements.md`: the hard requirements, each with its acceptance tests
- `architecture.md`: the API, the messages, and the pieces of each program
- `design.md`: the inside of each piece, with its unit tests
- `diagrams.md`: the design as diagrams
- `ux.md`: what the user sees
- `CLAUDE.md`: the decisions and why
