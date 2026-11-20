# Gangway diagrams

These show the first version as `blueprint.md` describes it. The MVP is the same flow on one machine, with no Tailscale yet.

## 1. Parts

Where each part runs and who starts each connection. An arrow points from the side that dials to the side that listens.

```mermaid
flowchart LR
  me(["Kostas"])

  subgraph aim["AI machine"]
    ai["AI tool with the skill"]
  end

  subgraph ctl["My machine on Tailscale"]
    api["Controller: REST API"]
    dash["Controller: dashboard"]
  end

  subgraph host["Host: Linux, plain user"]
    cli["cli-tool"]
    gate{"Gate: y or n"}
    cmd["Approved command"]
  end

  ai -- "request with session key" --> api
  cli -- "dials out over Tailscale" --> api
  api -. "request, down the open link" .-> cli
  cli --> gate
  gate -- "y" --> cmd
  me -- "y or n in the terminal" --> gate
  me -- "watches, ends a session" --> dash
```

The host listens on nothing. The dotted arrow is not a new connection: the request travels down the link the cli-tool opened.

## 2. One session

```mermaid
sequenceDiagram
  actor K as Kostas
  participant C as cli-tool on the host
  participant S as Controller
  participant A as AI tool

  K->>C: start
  C->>S: join Tailscale and connect
  Note over C: prints dots while it connects
  C->>C: make the session key
  C->>S: tell the controller the key
  C-->>K: paste line: API address and key
  K->>A: paste the line

  loop each request
    A->>S: run a command or read a file, with the key
    S->>S: check the key
    S->>C: the request
    C-->>K: show the exact request
    alt Kostas presses y
      K->>C: y
      C->>C: run as the plain user
      C->>S: output and exit code
      S->>A: output and exit code
    else Kostas presses n
      K->>C: n
      C->>S: refused
      S->>A: refused
    end
  end

  K->>C: Ctrl+C
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

  Idle --> Ended: Ctrl+C or the link is lost
  Asking --> Ended: Ctrl+C or the link is lost, nothing runs
  Running --> Ended: Ctrl+C or the link is lost, the command is stopped

  Ended --> [*]
```

- Only `y` in the Asking state leads to Running. There is no other way in.
- A key pressed in Idle or Running is thrown away. It does not answer the next request.
- A request that arrives in Asking or Running waits in line. Requests are asked one at a time, in order.
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

### 4b. The host drops while a prompt waits

```mermaid
sequenceDiagram
  actor K as Kostas
  participant C as cli-tool on the host
  participant S as Controller
  participant A as AI tool

  A->>S: run a command, with the key
  S->>C: the request
  C-->>K: show the exact request
  C--xS: the link is lost
  S->>S: end the session, forget the key
  S->>A: refused: the session ended
  Note over C: the cli-tool ends, nothing runs
```

### 4c. The AI hangs up while a prompt waits

```mermaid
sequenceDiagram
  actor K as Kostas
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

## 5. Trust

Who can only ask, and who decides.

```mermaid
flowchart LR
  subgraph ask["Can only ask"]
    ai["AI tool"]
    ctl["Controller"]
  end

  subgraph decide["Decides and runs: the host"]
    k(["Kostas at the terminal"])
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
| The controller | Ask. Read every result that passes through it. Drop or change a result on its way to the AI | Run anything without a `y`. Change what the prompt shows: the host shows the request it got and runs exactly that |

## For the reviewer

Three points the diagrams had to assume. Each one is open.

1. **The link.** The diagrams show one open link from the cli-tool to the controller, with requests pushed down it. I assumed a websocket.
2. **The key on the controller.** Diagram 2 has the cli-tool tell the controller the key. The controller could keep only a hash of it, so a memory dump of the controller does not give a usable key.
3. **The AI hangs up while a command runs.** Diagram 4c covers a waiting prompt only. For a running command I assumed it runs to the end and the result is dropped. The other choice is to stop the command.
