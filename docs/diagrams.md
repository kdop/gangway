# Gangway diagrams

These show the design as `blueprint.md` describes it. The one-machine mode is the same flow with no Tailscale.

## 1. Parts

Where each part runs and who starts each connection. An arrow points from the side that dials to the side that listens.

```mermaid
flowchart LR
  me(["User"])

  subgraph aim["AI machine"]
    ai["AI tool with the skill"]
  end

  subgraph ctl["Controller machine: on Tailscale, can also have a public address"]
    api["Controller: REST API, needs the session key"]
    dash["Controller: dashboard, needs the login"]
  end

  subgraph host["Host: Linux, plain user"]
    cli["cli-tool"]
    gate{"Gate: y or n"}
    cmd["Approved command"]
  end

  ai -- "request with session key" --> api
  cli -- "dials out over Tailscale" --> api
  api -. "request, down the open connection" .-> cli
  cli --> gate
  gate -- "y" --> cmd
  me -- "y or n in the terminal" --> gate
  me -- "signs in from a browser or phone, watches, ends a session" --> dash
```

The host listens on nothing and reaches the controller over Tailscale only. The AI, the browser and the phone reach the controller on its own address, which can be public. The dotted arrow is not a new connection: the request travels down the link the cli-tool opened.

## 2. One session

```mermaid
sequenceDiagram
  actor K as User
  participant C as cli-tool on the host
  participant S as Controller
  participant A as AI tool

  K->>C: start
  C->>S: join Tailscale and connect
  C->>S: host token from pairing
  S->>S: find the user this host belongs to
  C->>C: make the session key
  C->>S: send a hash of the key
  C-->>K: paste line: API address and key
  K->>A: paste the line

  loop each request
    A->>S: run a command or read a file, with the key
    S->>S: check the key
    S->>C: the request
    C-->>K: show the exact request
    alt the user presses y
      K->>C: y
      C->>C: run as the plain user
      C->>S: output and exit code
      S->>A: output and exit code
    else the user presses n
      K->>C: n
      C->>S: refused
      S->>A: refused
    end
  end

  opt the time is up, below the maximum
    C-->>K: ask: extend?
    alt the user presses y
      K->>C: y
      Note over C: the session gets more time
    else n, or no answer in time
      C->>S: end the session
    end
  end

  K->>C: Ctrl+C, or the maximum is reached
  C->>S: end the session
  Note over S: the key is dead
  Note over C: the host leaves Tailscale
```

## 3. The gate

The states of the cli-tool during a session.

```mermaid
stateDiagram-v2
  [*] --> Connecting: start
  Connecting --> Idle: connected, key shown
  Connecting --> Ended: Ctrl+C

  Idle --> Asking: a request arrives
  Asking --> Running: y
  Asking --> Idle: n, the AI gets refused
  Asking --> Asking: any other key, ignored
  Running --> Idle: the command ends, the result is sent

  Idle --> Extending: the time is up, below the maximum
  Extending --> Idle: y, the session gets more time
  Extending --> Ended: n, or no answer in time

  Idle --> Ended: Ctrl+C, the connection stays lost, or the maximum is reached
  Asking --> Idle: Ctrl+C, the request is refused
  Asking --> Extending: the time is up, the request is refused
  Asking --> Ended: the connection stays lost or the maximum is reached, nothing runs
  Running --> Idle: Ctrl+C, the command is killed, the AI is told
  Running --> Extending: the time is up, after the command ends
  Running --> Ended: the connection stays lost, the command is stopped
  Running --> Ended: the maximum is reached, after the command ends

  Ended --> [*]
```

- Only `y` in the Asking state leads to Running. There is no other way in.
- A key pressed in the first moment after a prompt is shown is thrown away too.
- A key pressed in Idle or Running is thrown away. It does not answer the next request.
- A request that arrives in Asking or Running waits in line. Requests are asked one at a time, in order.
- Extending is the question "extend?" when the session's time is up. Only the user at the terminal can answer it.
- When the time is up or the maximum is reached, a running command is not killed. It may finish, but it gets no more "wait more", so it is killed at its next time limit. New requests are refused from that moment.
- Two Ctrl+C close together lead to Ended from any state. The diagram leaves those arrows out to stay readable.
- "The connection stays lost" means it did not come back within the reconnect window. A shorter loss changes no state.
- Ended is final. There is no resume.

## 4. When things go wrong

### 4a. No live session for the key

The key is wrong, or the session has ended. The controller cannot tell these apart and does not need to.

```mermaid
sequenceDiagram
  participant A as AI tool
  participant S as Controller
  participant C as cli-tool on the host

  A->>S: run a command, with a key
  S->>S: no live session has this key
  S->>A: refused: no such session
  Note over C: nothing reaches the host
```

### 4b. The connection is lost while a prompt waits

```mermaid
sequenceDiagram
  actor K as User
  participant C as cli-tool on the host
  participant S as Controller
  participant A as AI tool

  A->>S: run a command, with the key
  S->>C: the request
  C-->>K: show the exact request
  C--xS: the connection is lost
  Note over C: the prompt stays, the cli-tool tries to connect again
  Note over S: new requests are refused with host_away

  alt the connection comes back in time
    C->>S: I am back, with host token and key hash
    S->>C: taken back
    Note over C,S: the same session carries on
  else it stays away past the reconnect window
    S->>S: end the session, forget the key
    S->>A: refused: the session ended
    Note over C: the cli-tool ends the session
  end
```

### 4c. The AI hangs up while a prompt waits

```mermaid
sequenceDiagram
  actor K as User
  participant C as cli-tool on the host
  participant S as Controller
  participant A as AI tool

  A->>S: run a command, with the key
  S->>C: the request
  C-->>K: show the exact request
  A--xS: the AI hangs up
  S->>C: cancel the request
  C-->>K: remove the prompt
  Note over C: nothing runs, the session stays open
```

If the AI hangs up while a command runs, the cli-tool kills the command. The session stays open.

## 5. Trust

Who can only ask, and who decides.

```mermaid
flowchart LR
  subgraph ask["Can only ask"]
    ai["AI tool"]
    ctl["Controller"]
  end

  subgraph decide["Decides and runs: the host"]
    k(["User at the terminal"])
    gate{"Gate in the cli-tool"}
    cmd["Command, as the plain user"]
  end

  ai -- "asks, with the session key" --> ctl
  ctl -- "passes the request" --> gate
  k -- "y or n" --> gate
  gate -- "only on y" --> cmd
  cmd -- "result" --> ctl
  ctl -- "result" --> ai
```

| If this is taken over | It can | It cannot |
|---|---|---|
| The AI tool | Ask for harmful commands. Read every result it asked for | Run anything without a `y` |
| A stolen session key | Ask, until the session ends | Run anything without a `y` |
| The login of a user | See that user's sessions and results in the dashboard. End that user's sessions | Run anything without a `y`. Sign in at all, unless the account is on the list |
| The controller | Ask. Read every result that passes through it. Drop or change a result on its way to the AI | Run anything without a `y`. Change what the prompt shows: the host shows the request it got and runs exactly that |

## 6. Pairing a host to a user

Done once per host, at `login`.

```mermaid
sequenceDiagram
  actor K as User
  participant C as cli-tool on the host
  participant S as Controller
  participant P as Login provider

  K->>C: login
  C->>S: ask for a pairing code
  S->>C: one-time code, dies in a few minutes
  C-->>K: show the code

  K->>S: open the dashboard
  S->>P: send the user to sign in
  P->>S: this is the account
  S->>S: is the account on the list?
  K->>S: type the code

  S->>C: this account claimed the host
  C-->>K: show the account, ask y or n
  alt the user presses y
    K->>C: y
    C->>S: accepted
    S->>C: host token for this user
    Note over C: the token is kept like the auth key
  else the user presses n
    K->>C: n
    C->>S: refused
    Note over S: the code is dead, the host stays unpaired
  end
```

- An account that is not on the list is refused at sign-in and never reaches the code.
- A used code or an old code is refused.
