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
| `acceptance/` | The acceptance tests of `docs/requirements.md`. They start both real programs. Go |
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

## Put the controller on a server

No script for it is in this repo. The one in use is in the `vps-automation` repo, on branch `gangway`: it builds the controller from the newest `main`, copies it over, runs it as a service of a plain user on localhost, and sets up nginx. This is for now.

## Make a release

A release is made on GitHub from a tag that starts with `v`:

```
git checkout main && git pull
git tag v0.1.0
git push origin v0.1.0
```

GitHub then runs the tests, builds both programs for Linux on x86_64 and on aarch64, and puts the four files and `SHA256SUMS` on the releases page, `https://github.com/kdop/gangway/releases`. A tag on a commit whose tests fail gives no release. The workflow is `.github/workflows/release.yml`.

To check a downloaded file by hand, put it next to `SHA256SUMS` and run:

```
sha256sum --check --ignore-missing SHA256SUMS
```

`install.sh` reads the release: it takes `gangway-linux-amd64` or `gangway-linux-arm64` and checks it against `SHA256SUMS`. So the names of those files must stay as they are. The script is served from the `main` branch, not from a release, so a change to it is live once it is merged.

## Run the tests

```
cd cli && go test ./...
```

The same goes for each folder that holds Go code. On GitHub, a pull request runs only the tests of the folders it changes: a change in `shared/` runs them all, a change in `cli/` runs `cli` and `acceptance`, and a change to the docs that tests read runs one short check. A change to other docs runs none. A push to `main` and a release run them all.

To see how many lines of each package its tests run, add `-cover`: `go test -cover ./...`. On GitHub the same numbers are in the summary of each run. There is no pass mark: the check that every requirement has a test is the guard.

## Read next

- `docs/blueprint.md`: what it is and how the parts fit
- `docs/releases.md`: the versions, the state of each, and what has no version yet
- `docs/requirements.md`: the hard requirements, each with its acceptance tests
- `docs/architecture.md`: the API, the messages, and the pieces of each program
- `docs/design.md`: the inside of each piece, with its unit tests
- `docs/diagrams.md`: the design as diagrams
- `docs/ux.md`: what the user sees
- `CLAUDE.md`: the decisions and why
