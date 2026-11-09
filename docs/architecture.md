# Architecture

Togo has three parts: the app on iPhone, iPad and Apple Watch; the `togo` helper on each host; and a push relay that forwards notifications to Apple. The app works without the other two as a plain SSH, Mosh or Eternal Terminal terminal.

```
iPhone / iPad / Watch                                 Host (macOS or Linux)
+---------------------------+  SSH exec: togo serve --stdio  +--------------------------+
| Togo app                  |<------ JSON-RPC (control) ---->| togo helper (Go)         |
|  chat views, terminal     |<-- SSH, Mosh or ET: tmux attach| sessions: tmux -L togo   |
|  TogoKit, TogoPush        |                                |  agent adapters, hooks   |
|  Watch app, widgets,      |                                |  git, files, ports       |
|  notification extension   |                                |  push sender (sealed)    |
+-------------^-------------+                                +------------+-------------+
              | APNs (ciphertext)                                         | HTTPS (ciphertext)
              +--------------------- Apple APNs <---- relay (Worker) <----+
```

## The two connections

**Control.** The app opens an SSH exec channel that runs `togo serve --stdio` on the host. Over it the two sides speak JSON-RPC 2.0, each message framed by a 4-byte big-endian length, capped at 1 MiB. The protocol is versioned and starts with a `hello` that negotiates capabilities. The schema and shared fixtures live in [protocol/](../protocol). The helper never listens on a network port; SSH does the authentication. The app keeps one control connection per machine and shares it between the session list, chat views and git and file screens (`apps/ios/Togo/App/HelperChannels.swift`). A network change drops it, and every feed reopens from its cursor over a new one.

**Terminal.** The terminal byte stream is a separate connection that attaches to a tmux session. The app renders it with SwiftTerm behind a `TerminalEngine` protocol in `packages/TogoTerminal`. Auto picks Mosh, then SSH. Eternal Terminal is used only when you choose it for a machine where `etserver` was found when the machine was added, and never for a machine behind a jump host, since ET dials the machine directly.

**Routes.** A machine can have several addresses: the one you typed, the LAN and Tailscale addresses its helper reports, and the last one that worked. The app races them and keeps the first that completes an SSH login; a host key mismatch on any route stops the whole race (`packages/TogoKit/Sources/TogoKit/Link/RouteRace.swift`). Mosh and ET aim at the address that won. A machine that may be asleep can be woken with Wake-on-LAN, and the app waits for its SSH port before connecting. When the phone goes offline the app says so once and reconnects when a network returns. A Mosh session roams across networks, and one whose server has gone attaches again.

## Sessions

Each Togo session is a tmux session on a dedicated socket, `tmux -L togo`, so the helper never touches the user's own tmux server. Sessions on the default socket are listed and can be attached, but not managed. Sessions get short numbers, called bib numbers after the numbers runners wear in a race, that the app, the desk commands (`togo new`, `togo ls`, `togo attach`) and notifications share.

The helper starts tmux and every other program with an argv array. Session names, folders and agent commands are passed as arguments and never joined into a shell command line.

## The daemon

`togo daemon` holds state shared by all connections: sessions, pending approvals, paired devices. It listens on a unix socket in the user's state directory, created with mode 0600 inside a 0700 directory, and drops any connection whose peer uid differs from its own. `togo serve --stdio`, agent hooks and the desk commands all talk to it through that socket. `togo service install` runs it at login under launchd or systemd.

## Agents

Togo knows 19 agents and a plain shell. Each has an adapter in `helper/internal/harness/adapters`: a YAML file that says how to recognise the agent, which of its own structured sources to read (hooks, the status line, transcript files, an app server or RPC mode, a local database) and how to apply actions, plus Go mappers that turn those sources into chat items and a mission snapshot. The daemon's harness engine (`helper/internal/harness`) runs the adapters and serves them to the app through the `harness.*` methods, so the app shows a native chat view with approvals, questions, model and mode controls where the agent supports them. Everything an adapter reads is redacted before it leaves the helper (`helper/internal/harness/redact`). The terminal stays one tap away, and an agent without an adapter, or a source that fails, falls back to it.

## Knowing when an agent needs you

Two sources feed the daemon:

- **Hooks.** For agents that publish structured events (Claude Code, Codex, Amp, Auggie, Copilot CLI, Cursor CLI and Droid), `togo hooks install` adds hook entries that run `togo hook <agent> <event>`. The registry of what each agent needs lives in `helper/internal/hooks`. Other agents report through their own protocol, a server or their files, and need no setup.
- **The screen watcher.** For every agent, the helper taps each Togo pane and looks for a terminal bell, OSC 9 and OSC 777 notifications, output going quiet, and prompt shapes it knows, such as numbered menus and `[y/n]`.

## Approvals

Approvals race. The agent's own prompt stays live at the desk while the phone shows the same request. Whichever side answers first wins. Before typing a phone answer, the helper captures the pane again under the pane's lock and checks that the prompt is still the one on screen; if it changed, nothing is typed and the request closes. A one-tap answer never picks an "always allow" option.

When the app is open with a live control channel, answers go over SSH. From a notification, the lock screen or the Watch, the device signs the answer with its approval key, seals it to the host and posts it to the relay mailbox. The helper keeps one outbound WebSocket to the relay, reads the mailbox, checks the signature, freshness and single use, and then applies the answer exactly as above.

## Pushes

At pairing, over the authenticated SSH channel, the phone and the host exchange X25519 public keys, and the phone registers its Ed25519 approval key. Each push is sealed to one device (X25519, HKDF-SHA256, ChaCha20-Poly1305) and sent to the relay, which hands it to APNs. The notification service extension on the phone opens it. Byte layouts are fixed by test vectors in `protocol/fixtures/crypto`, checked by both the Go and Swift code.

Live Activity and Dynamic Island updates are rendered by iOS straight from the APNs payload, without the notification extension. Their plain fields are status only: session number, agent name, status, waiting count, start time, the pending request id and, for an ended session, one word for why. The session title, its current step, the question and the command waiting for approval travel in a context field sealed to the one device the same way as an alert, which the widget opens on the phone (`helper/internal/push/livectx.go`). Command text is never sent in the clear. While a device's activity runs, its update stands in for that device's alert push for the same event (a request, or done, ended or failed once nothing else waits or works): the update carries an alert so iOS lights the screen and sounds, worded by the relay from the plain status alone, and the lock screen card answers with the same Approve and Deny as the notification. A device with no running activity, or a Watch, gets the regular push; a device just sent a start gets it if the activity has not registered within 10 seconds (`send` in `helper/internal/push/sender.go`). While an activity stands in, the Watch gets the request as its own alert, since no phone alert is mirrored to it, and the phone's app gets a silent copy so its widget and Watch replies know the request. When an activity ends or the user dismisses it, the phone reports its update token through the mailbox (`liveActivityEnded`), and the regular push resumes.

## The relay

The relay in [relay/](../relay) is a Cloudflare Worker. It holds the APNs key, forwards sealed pushes, rate-limits callers, and runs one Durable Object mailbox per host for answers, which deletes messages on acknowledgement or after 10 minutes. The helper can also leave a will with its mailbox: pushes it sealed ahead of time, which the relay sends once if the helper's connection goes quiet, so the phone learns the machine went silent. The relay cannot open anything it stores. Anyone can deploy their own and point the app and helper at it.

## Repository layout

| Path | Contents |
|---|---|
| `apps/ios` | App, Watch app, widgets and notification extensions; XcodeGen `project.yml` |
| `packages/TogoKit` | Protocol models, SSH, keys, routes, host monitor, chat models, transports (SSH, ET) |
| `packages/TogoTerminal` | Terminal engine and key bar |
| `packages/TogoMosh` | Mosh client |
| `packages/TogoPush` | Push crypto, pairing store, notification categories |
| `helper` | The `togo` command, daemon, agent adapters and hooks |
| `relay` | The push relay and answer mailbox |
| `protocol` | JSON-RPC schema and fixtures |

For the threat model, see [security.md](security.md).
