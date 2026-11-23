# Gangway

Remote access for AI, with just-in-time approval of commands.

An AI tool such as Claude Code asks to run a command or read a file on a remote machine. A person at that machine sees the request and says yes or no. Nothing runs without a yes.

It exists because SSH gives an AI a full shell with no say over the commands.

## Status

The first version, the MVP, is built: the three parts work together on one machine. The next step is built too: a host reaches the controller over Tailscale. It passes its tests on a stand-in for the Tailscale network, and it has run on a real one, with the controller on a server on the internet.

The design is written down in three levels, each with its tests: the requirements, the architecture, and the inside of each piece. The pieces are built one at a time, and every test in the code names the requirement it proves.

## Known limits

- **Chat windows on the web probably cannot use it as it is.** The AI must be able to send a POST request with the key in a header. A tool with a shell can, such as Claude Code. A chat on the web usually can only read a web address.

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

## Use it over Tailscale

The controller and each host join your Tailscale network from inside the program. Nothing else is installed, and a Tailscale that is already on the machine is left alone.

1. In the Tailscale admin page, make two auth keys. The one for the controller: reusable and not ephemeral. The one for the hosts: reusable and ephemeral, with a tag that your Tailscale rules let reach the controller and nothing else.
2. On the controller machine, once: `gangway-controller login`, and paste the controller's key. Then start it: `gangway-controller`. Add `-public-address https://…` when the AI reaches it at another address than the one it listens on.
3. On a host, once: `gangway login`, and paste the hosts' key. Then start a session: `gangway`.

Until pairing is built, any device that your Tailscale rules let reach the controller can start a session of its own. The rules are the guard.

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
