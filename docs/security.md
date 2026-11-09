# Threat model

This document describes what Togo protects, from whom, how, and where it stops. File paths point at the code that does the work. Report problems as described in [SECURITY.md](../SECURITY.md).

## Assets

- **Hosts.** A shell on your machine, where agents can read and change your code.
- **The SSH key** on the phone, which opens that shell.
- **The approval key** on each paired phone and Watch, which can answer one pending request.
- **Notification and chat content.** Prompts, commands, screen snippets and agent transcripts from your sessions.
- **The helper binary** and its updates, which run as you on every host.

## Actors

| Actor | Can | Cannot |
|---|---|---|
| Network attacker | See and alter traffic between phone, host, relay and Apple | Read or forge SSH, Mosh or sealed push traffic |
| Relay operator | See sealed envelopes, timing, sizes, host ids and APNs device tokens; read Live Activity status fields | Open notifications or answers, or forge an answer the helper accepts |
| Someone holding your locked phone | Read lock-screen notifications if iOS previews allow it | Approve without unlocking (default), open a terminal, use the SSH key |
| Someone holding a stolen approval key | Answer a request that is pending right now | Type arbitrary input, pick "always allow", open a shell |
| Malicious repository content | Put hooks, filters and odd file names in a repo you open | Run them through Togo's git views |
| Malicious release mirror | Serve any file under a release URL | Get the install script or `togo update` to install it |

## Mitigations

### Getting into the host

- The helper opens no network port of its own. The app reaches it through an SSH exec channel, so SSH authentication and host-key checks cover it. The daemon's unix socket is mode 0600 in a 0700 directory, and connections from another uid are dropped (`helper/internal/sockd`).
- The SSH private key is created on the device: in the Secure Enclave (P-256) where available, otherwise in the Keychain. Either way it is device-only and needs Face ID, Touch ID or the passcode for each use (`packages/TogoKit/Sources/TogoKit/Keys`). It is never synced.
- Togo never adds to `authorized_keys`. The app shows its public key line and fingerprint, and you add it on the host yourself. When you revoke a phone, the helper removes only the lines Togo's own keys use (the exact key the phone sent at pairing, under a `togo@` or `togo-` comment), keeps a copy of the file as `authorized_keys.togo-backup`, and leaves every other line alone. If the phone never sent its key, revoke removes nothing and lists the Togo key lines for you to remove by hand.
- Host keys are trusted on first use after the app shows the fingerprint. A changed host key stops the connection, and when the app races a machine's routes, a mismatch on any route ends the whole race rather than moving on to the next address (`packages/TogoKit/Sources/TogoKit/Link/RouteRace.swift`).
- Routes the helper reports (LAN and Tailscale addresses) only add addresses to try. Each one still goes through the same SSH login and host-key check.

### Pushes and the relay

- Every notification push is sealed to one device with X25519, HKDF-SHA256 and ChaCha20-Poly1305 (`helper/internal/seal`, `packages/TogoPush`). The phone's notification service extension opens it. The relay forwards ciphertext and does not log it.
- A Live Activity update's session title, current step, question and command are sealed the same way into its context field, for the one device it goes to (`helper/internal/push/livectx.go`). The widget opens it on the phone. Command text never goes in a plain field (`mask` in `helper/internal/push/livesend.go`).
- A will the helper leaves with its mailbox holds pushes sealed in advance; the relay can send them but not read them (`relay/src/will.ts`).
- Answers sent through the relay are sealed to the host and signed by the device's Ed25519 approval key. The helper checks the signature against the registered key, rejects revoked devices, answers older than 10 minutes and anything seen before (`helper/internal/mailbox`).
- The mailbox deletes messages once the helper acknowledges them, and after 10 minutes otherwise (`relay/src/mailbox.ts`). The helper reaches it over an outbound WebSocket.
- Pairing keys are exchanged over the authenticated SSH channel, never through the relay.

### Approvals

- An answer names one request id. The helper applies it only while that request is pending, and before typing it captures the pane again and checks that the same prompt is still on screen (`helper/internal/approval`). A stale or replayed answer types nothing.
- A one-tap answer never selects an "always allow" option, and text answers are refused on the relay path; both stay available in the app over SSH.
- The approval key is readable after first unlock without biometrics, so the Watch and lock screen can use it. That is why it can do so little.
- Approve on a lock-screen notification requires unlocking the device by default (`packages/TogoPush/Sources/TogoPush/NotificationCategorySet.swift`). A setting turns this off for the phone. The Watch always requires it.

### Repository content

- Every git call from the helper runs with `core.hooksPath=/dev/null`, and with every filter driver and config-defined hook the repository declares switched off for that call (`helper/internal/gitx/git.go`, `helper/internal/gitx/neutral.go`). Commits made from the app use `--no-verify` and `--no-gpg-sign`.
- The helper starts tmux, git and every other program from an argv array. Session names, paths and agent names never pass through a shell (`helper/internal/tmux/client.go`).
- The helper reads an agent's hooks, status line, transcripts and session files through that agent's adapter (`helper/internal/harness/adapters`). Secrets are stripped from everything an adapter reads before it leaves the helper (`helper/internal/harness/redact`). The helper never reads agent credential files.
- Adapter files say what to read and how to act, but never where to connect: for agents that serve an API, the helper picks a loopback port or a unix socket under the Togo home, and a random server password where the agent supports one (`helper/internal/harness/engine/endpoint.go`).
- An API key you enter in the app for an agent's provider goes over the SSH control channel to that agent's own store. Togo does not keep it.

### Releases

- `SHA256SUMS` is signed with minisign. The trusted comment names the version, so a valid older file cannot stand in for a newer release. The install script (`scripts/install.sh`) and `togo update` pin the public key, check the signature and the hash, and write nothing to the install directory if either fails. On macOS the binary is also signed with a Developer ID and notarized.

## Known limits

- **Live Activity and Dynamic Island status fields are not encrypted.** iOS renders them directly from the APNs payload, without the notification extension. Each update carries the session number, agent name, status, waiting count, start time, pending request id and, for an ended session, one word for why, in plaintext, and the relay and Apple can read them (`helper/internal/push/live.go`). Everything else, including command text, is in the sealed context.
- **What the relay sees.** It sees when pushes and answers happen, their sizes, your host id and your APNs device tokens. It cannot read their content. Run your own relay if that metadata matters to you.
- **Lock-screen previews.** Decrypted notifications show prompt text on the lock screen when iOS previews are set to Always. Set previews to When Unlocked to hide them.
- **Every phone you add holds a key with full shell access as your user.** Togo does not restrict what that key can run on the host. A stolen phone together with its passcode can do anything you can. Remove the device with `togo devices revoke`, which also removes its key line from `authorized_keys`.
- **Mosh** runs `mosh-server`, which listens on a UDP port between 60000 and 61000 while a session is open. That is Mosh's own design; Togo's helper still listens on nothing.
- **Eternal Terminal.** If you run `etserver` on a host, it listens on TCP port 2022 while it runs. Togo does not install or start it. Togo uses ET only when you pick it for a machine, never by itself, because its client cryptography has not been audited. It is never used through a jump host.
- **Agent servers.** An agent that serves an API, such as OpenCode, listens on loopback or a unix socket while its session runs. Other programs running as you on the host can reach it, as they could reach the agent anyway.
- **The helper runs as you.** An agent, or anything else running as your user, can already do what the helper can. Togo does not sandbox agents.
- **The relay host** is trusted for availability. If it is down, pushes and lock-screen answers stop, but the app over SSH keeps working.
