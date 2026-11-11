<!-- The animated hero plays inline everywhere GitHub renders this file. For a sharper, smaller
     inline player, upload .github/assets/hero.mp4 through GitHub's web editor (drag it into this
     file) and put the user-attachments URL it gives you here in place of the image. -->
<p align="center">
  <img src=".github/assets/hero.webp" alt="Togo on iPhone, on one OpenCode session: the session list, the chat streaming, the same session's terminal, the Lock Screen asking to run npm test and Approve tapped on it, then the seven changed files and the diff of render.ts" width="100%">
</p>

<p align="center"><strong>Every coding agent, on your phone.</strong></p>

<p align="center">
  Togo is an iPhone, iPad and Apple Watch app for the coding agents running in tmux on your own Mac or Linux machine.<br>
  Follow a session, answer it from the Lock Screen or your wrist, read the diff and commit. The code never leaves your machine.
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#supported-agents">Agents</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#privacy-and-security">Privacy</a> ·
  <a href="#build-from-source">Build</a> ·
  <a href="https://togoapp.dev">togoapp.dev</a>
</p>

> [!NOTE]
> The `togo` helper works today on macOS and Linux. The app is not on the App Store yet and there is no public test build.

You're in line for coffee and the agent you left on the big refactor has stopped. It wants to run a database migration and it won't without you. Your phone buzzes. You read the command on the Lock Screen and tap Deny. By the time your order is up, the agent has moved on to a safer plan, still in tmux, on your own machine.

## What it does

Coding agents got good enough to leave alone for an hour, and then they stop to ask something. Togo leaves the agent where it already runs, in a terminal on a machine you own, and gives your phone a proper way in.

<table>
  <tr>
    <td width="33%"><img src=".github/assets/screen-chat.webp" alt="Native chat for an OpenCode session, with a request to run npm test"></td>
    <td width="33%"><img src=".github/assets/screen-terminal.webp" alt="The same session's terminal, live in tmux"></td>
    <td width="33%"><img src=".github/assets/screen-review.webp" alt="A file diff in Review"></td>
  </tr>
  <tr>
    <td>A native chat for 16 agents. Tool steps fold into one line per turn and approvals wait inline.</td>
    <td>The real terminal, one tap away. The agent's own interface in tmux stays the source of truth.</td>
    <td>Review what changed: diffs, history, files and ports. Stage and commit from the phone.</td>
  </tr>
</table>

- When an agent waits on you, a Live Activity lights the Lock Screen and the Dynamic Island, and your Watch taps. Approve or deny from any of them.
- Every native agent's questions and approvals reach the phone as a needs-you, with a push, the Lock Screen and the Watch. A multi-choice question is never offered as Yes or No.
- In the terminal, a row above the keyboard holds prompt keys, your shortcuts, Esc, Ctrl, Mode and Stop, and Hide keyboard. A directional controller stands in a column on iPhone and iPad while you type, and in the dock on iPhone Duo. Settings, Keyboard changes them. A long press selects a word.
- Chat text selects by the word and copies exactly, Markdown tables included. Paste keeps every character, and a pasted screenshot or photo becomes an attachment.
- Start a session from the phone: pick a machine, a folder and an agent, and it starts in tmux. Sessions you start at the desk show up on the phone, and the other way round.
- Mission control pulls up an agent's subagents, background shells, plan, changes, ports and status, for agents that report them.
- Model, mode and effort pickers work through the agent itself, by its own commands or its API, so the terminal shows the same choice.
- On iPad, and on iPhone Duo when it is open, the session list stays beside the session. Held as a tabletop, the Duo keeps the terminal above the hinge and the keyboard below.
- Togo tries the machine's local name, its home network address, Tailscale and any saved address at once, and keeps the first that answers with the host key it already knows.
- The helper is scriptable: `togo ask "Ship it?" --options yes,no` waits for your answer on the phone, and `togo notify`, `togo job` and `togo awake` cover the rest of the long-task chores.

<p align="center">
  <img src=".github/assets/devices.webp" alt="Togo on iPad: the session list, a finished OpenCode turn and mission control with its changes and status. Beside it, an OpenCode request on Apple Watch" width="100%">
</p>

Without the helper, any machine you can reach over SSH still works as a full terminal with its own tmux sessions.

## Install

Togo has two parts: the `togo` helper on the machine your agents run on, and the app on your phone.

### The helper

One Go binary for macOS and Linux, arm64 and amd64. It needs tmux 3.2 or newer and installs to `~/.togo/bin`, where the app looks first.

```sh
curl -fsSL https://togoapp.dev/install.sh | sh
```

The script installs from the latest GitHub release and sets up the login service (`togo service install`), so needs-you alerts arrive while the app is closed; pass `--no-service` to skip it (`curl -fsSL https://togoapp.dev/install.sh | sh -s -- --no-service`). It checks the minisign signature on `SHA256SUMS` and the archive's checksum, and writes nothing until both pass. Until the first release is tagged, install from source with Go 1.26 or newer:

```sh
GOBIN="$HOME/.togo/bin" go install github.com/gettogo/togo/helper/cmd/togo@latest
```

Then:

```sh
togo doctor            # checks tmux, mosh-server, the firewall and Tailscale SSH; changes nothing
togo service install   # only needed after --no-service; runs the daemon at login
togo hooks install     # sets up every installed agent that takes hooks; backs up each file once first
togo new claude --dir ~/code/project
```

`togo ls` lists sessions and `togo attach 1` joins one from another terminal. Run `togo` with no arguments for every command. `togo uninstall` takes the hooks and the login service back out.

### The app

<!-- TODO(owner): replace with the App Store badge and link once the listing is live. -->
App Store link coming with the first release.

In the app you add the machine by hostname or Tailscale name, put the app's public key in `~/.ssh/authorized_keys`, and check the host key fingerprint it shows the first time. Pairing for notifications happens over that same SSH connection. There is no code to type and no port to open. A demo machine lets you look around before you have set one up.

## Supported agents

These 16 agents get a native chat. The app draws it from what the agent already writes down: its hooks, its session log or its local API. An agent outside the version range its adapter was checked against opens in the terminal instead of guessing.

<!-- Generated from helper/internal/agents/agents.go and helper/internal/harness/adapters/*/*.yaml (the capabilities line). Regenerate when an adapter changes; OpenCode, OMP and Pi lead, the rest follow the registry order. -->

| Agent | Command | Native controls | Togo reads |
|---|---|---|---|
| OpenCode | `opencode` | model, mode, approvals, questions, resume | its local HTTP API and event stream |
| OMP | `omp` | model, effort, questions | session log |
| Pi | `pi` | model, effort, resume | session log |
| Claude Code | `claude` | model, mode, effort, approvals, questions, resume | hooks, status line, transcript |
| Codex | `codex` | model, mode, effort, approvals, questions, resume | its app-server, as a second client |
| Copilot CLI | `copilot` | model, approvals, questions, resume | hooks |
| Cursor CLI | `cursor-agent` | model, approvals | hooks |
| Aider | `aider` | model, mode | chat history file |
| Amp | `amp` | approvals | a Togo plugin |
| Cline | `cline` | approvals, questions, resume | session file |
| Crush | `crush` | mode, questions, resume | its client-server API |
| Droid | `droid` | approvals, questions, resume | hooks |
| Goose | `goose` | mode, approvals, questions, resume | session database, read-only |
| Kilo Code | `kilo` | model, mode, approvals, questions, resume | its local HTTP API and event stream |
| Kimi Code | `kimi` | model, mode, approvals, questions, resume | wire log |
| Mistral Vibe | `vibe` | mode, approvals, questions, resume | session journal |

Any other agent, Auggie, Kiro and OpenHands included, runs in Togo as a terminal. It can still reach you: a screen watcher spots a numbered menu or a y/n prompt in the pane and asks you, and your answer is typed back as the keys that pick it. Plain shells with prompt marks (OSC 133) get each command as its own block.

Adapters are YAML plus a small Go mapper in [`helper/internal/harness/adapters`](helper/internal/harness/adapters). A new agent is a good first contribution.

## How it works

<p align="center">
  <img src=".github/assets/how-it-works.png" alt="The phone reaches the helper and tmux on your machine over SSH or Mosh. The helper sends sealed pushes through the relay to Apple's push service, and answers from the Lock Screen or Watch come back signed through the relay." width="100%">
</p>

- Sessions are tmux sessions on their own socket, `tmux -L togo`. Your own tmux server is left alone.
- The app talks to the helper through an SSH exec channel (`togo serve --stdio`). The helper opens no listening port of its own.
- The terminal is a plain SSH or Mosh connection attached to the session.
- Adapters read what each agent already records and act by typing its documented commands, relaunching it with flags, or answering through its approval path.
- When an agent waits, the helper seals a push for each paired phone and sends it through the relay at `relay.togoapp.dev`. Answers from the Lock Screen or the Watch come back signed, through a short-lived mailbox the helper reads over an outbound connection.

## Privacy and security

- There is no account. Your phone connects to machines you own with an SSH key made on the device. The private half stays in the Secure Enclave or the device-only Keychain.
- Each push is sealed on your machine for one device (X25519, HKDF-SHA256, ChaCha20-Poly1305). The relay forwards ciphertext it cannot open.
- Live Activities are the exception iOS imposes: it renders them straight from the push, so their status fields travel in the clear. That is the session number, the agent's name, its status, how many requests wait and when it started. Command text rides in the sealed part.
- Every approval from the Lock Screen or the Watch is signed by that device's Ed25519 key, checked for freshness and used once. The relay path carries approve and deny only; typed replies and "always allow" stay in the app.
- Host keys are pinned. A route that shows a different key drops out before any credential is sent.
- The relay is a small Cloudflare Worker in [`relay/`](relay), and the app can point at your own.
- The helper never reads an agent's credential files and never logs secrets. The app has no analytics, ads or crash reporting.

The full model is in [docs/security.md](docs/security.md). Report vulnerabilities privately; see [SECURITY.md](SECURITY.md).

## Build from source

| Path | What |
|---|---|
| `apps/ios` | The iPhone, iPad and Watch app (SwiftUI, Xcode project generated by XcodeGen) |
| `packages` | Swift packages: TogoKit, the terminal, Mosh and push crypto |
| `helper` | The `togo` helper (Go) |
| `relay` | The push relay (Cloudflare Worker, TypeScript) |
| `protocol` | The JSON-RPC schema and fixtures both sides test against |

```sh
make build lint test   # the helper: build, gofmt and go vet, tests
make relay-test        # the relay
make ios-bootstrap     # on a Mac with Xcode: simulators, signing team, Togo.xcodeproj
make ios-test
```

The helper, the adapters, the relay and the protocol build and test on macOS or Linux without an Apple developer account. The app needs Xcode and iOS 26 or later.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) first. Sign off every commit with `git commit -s`. Adapters, terminal themes and keyboard presets are the easiest places to start. Questions go to [Discussions](https://github.com/gettogo/togo/discussions).

## Why the name

In the winter of 1925, relays of dog teams carried diphtheria serum across Alaska to Nome. Togo led Leonhard Seppala's team, and they covered about 260 miles, far more than any other team on the run, including a crossing of the sea ice on Norton Sound. Balto led the last leg into Nome and got the statue in Central Park. For decades Togo was mostly left out of the story. He did the long haul in the dark, which is the job we had in mind.

## Licence

The app (`apps/` and `packages/`) is GPLv3 with an app store permission; see [LICENSE](LICENSE). The helper, relay and protocol are MIT; see [helper/LICENSE](helper/LICENSE), [relay/LICENSE](relay/LICENSE) and [protocol/LICENSE](protocol/LICENSE).
