# Choosing a mobile agent client

Togo connects an iPhone or iPad to coding agents on your own Mac or Linux machine. It combines native chat for 16 harnesses with the same session's terminal, git review and commits, and approvals from the Lock Screen or Apple Watch. The app, helper and relay are free and open source; model access and hosting are separate.

Checked on **13 November 2026** against each project's own documentation, linked below. Features and pricing change; corrections are welcome.

## What Togo adds to your harness

Togo runs your installed agent in its own tmux server, `tmux -L togo`. The Go helper reads the agent's hooks, transcript or API and maps it into native messages, tool activity, questions and controls. There is no requirement for an agent to implement ACP. Integration depth varies: [the agent table](../README.md#supported-agents) lists the controls, and the [adapter definitions and mappers](../helper/internal/harness/adapters) show how each works.

The phone sends chat and control requests through an SSH exec channel. The helper needs no additional listening network port. Terminal access uses SSH or Mosh and joins the same session; at the desk, `togo attach 1` joins it too. A separate relay carries encrypted notifications and signed approval replies. See the [architecture](architecture.md) and [security model](security.md) for the boundaries, including the Live Activity status metadata that is not encrypted.

Local model inference stays wherever your harness runs it. Togo uses that harness's existing model configuration; it does not run the model on the phone. You still need a reachable, awake host.

## Moshi

Moshi is a close comparison: it also combines a mobile terminal, transcript-backed chat and a direct connection to your host. Its [Chat View](https://getmoshi.app/docs/chat-view) requires Pro or remaining trial uses. Its [Free vs Pro documentation](https://getmoshi.app/docs/subscription) also places Mosh, diff review and Watch actions in Pro, with five trial inbox actions before a paid unlock. Togo includes those features in its free app, with source for the app, helper and relay available here.

Agent coverage differs in both directions. Moshi's [integration tiers](https://getmoshi.app/docs/hooks) list Aider, Cline, Crush, Kilo Code and Mistral Vibe as terminal-only. Togo has native chat adapters for those five. Moshi lists other chat integrations that Togo does not have, so a total agent count alone is not a useful measure.

Moshi also supports [other multiplexers](https://getmoshi.app/docs/multiplexer), including Herdr, and ships on Android. Togo currently manages tmux sessions on Mac or Linux and ships on Apple mobile devices. Herdr support is planned.

## Paseo

Paseo is also [free and open source](https://paseo.sh/). It has [broad native and ACP agent support](https://paseo.sh/agents), including OpenCode, OMP and Pi. Price and support for those harnesses are shared strengths.

The distinction is the workflow. Togo gives you mobile chat and controls over SSH around a running tmux session that a plain terminal can join. Paseo provides a [full development environment](https://paseo.sh/docs/why) with desktop, mobile and web clients, worktrees, orchestration and a desktop browser. It can also run headless without its desktop app.

Paseo's [connectivity docs](https://paseo.sh/docs/connectivity) distinguish desktop/CLI SSH tunneling from mobile access: its phone clients use an end-to-end encrypted relay or connect directly to the daemon over Tailscale. Togo's phone opens the SSH control channel itself; its relay handles notifications and approval replies. Both offer direct access to your own machine, with different connection setups.

## HAPI

HAPI offers a free Web/PWA client and paid iOS/Android app downloads. Its [native-app guide](https://hapi.run/docs/guide/native-apps) says the remote terminal is currently available through the web, alongside features such as Work Graph and realtime voice. Native app source is available for building your own version.

Togo's free App Store build includes native chat and a full terminal together. HAPI may fit better if you want browser or Android access, or support for older phones: its iOS app supports iOS 17, while Togo requires iOS 26.

## T3 Code, Orca and Happy

These projects combine multiple harnesses with development and orchestration workflows.

| Project | What to consider |
|---|---|
| **[T3 Code](https://github.com/pingdotgg/t3code)** | MIT licensed, with desktop, web, iOS and Android clients. Its current agent list includes OpenCode and Pi as well as ACP support. Threads can use worktrees, and its git workflow includes pushing and opening pull requests. Togo focuses on mobile access to tmux sessions over SSH; task worktrees and a native pull request workflow are roadmap items. |
| **[Orca](https://orca.dev/docs/ways-to-run)** | Offers worktrees, orchestration, SSH hosts and a shared remote runtime for desktop, browser and mobile. A headless server is supported, so its desktop app is not mandatory. Its [structured chat](https://orca.dev/docs/agents/native-chat) includes OpenCode, OMP and Pi; integration depends on the session type. Choose it for a broader development environment, or Togo for its Apple app and SSH/tmux workflow. |
| **[Happy](https://happy.engineering/welcome/)** | Free and MIT licensed, with its own runtime, multiple harnesses, subagents, scheduling and mobile access. Its core is free; hosted phone voice has a separate quota/subscription, with a bring-your-own ElevenLabs option. Togo keeps the agent in tmux and exposes the same running terminal. It does not offer Happy's orchestration or conversational voice workflow. |

## Termius, Blink Shell and ShellFish

A general-purpose terminal is enough if you mainly want the agent's TUI. Togo adds per-agent native chat and controls, git review and remote approvals to that workflow.

| Project | What it offers |
|---|---|
| **[Termius](https://www.termius.com/pricing)** | A free Starter tier with SSH/SFTP and paid plans for additional workflows, sync and collaboration. It serves general server administration and teams, beyond Togo's coding-agent focus. |
| **[Blink Shell](https://blink.sh/)** | An iPhone/iPad terminal with SSH, Mosh, local Unix tools and VS Code integration. Its site lists a trial followed by a paid annual plan. Choose it when terminal and editor access are the main job. |
| **[ShellFish](https://shellfishapp.com/)** | A native terminal and file workflow for iOS and Mac, with paid Pro unlocks. It also has [agent integrations](https://shellfishapp.com/help/agentic-coding) for OpenCode, Claude Code and Codex alerts, so agent awareness is not unique to Togo. Togo's additional focus is native conversation views and actionable requests across its supported harnesses. |

## KittyLitter and first-party remotes

**[KittyLitter](https://kittylitter.app/)** is free and open source, with iOS/Android clients, SSH and optional Alleycat pairing. Its site lists OpenCode, Pi, Claude, Codex and Droid. Togo has 16 chat adapters, including OMP, Aider, Cline, Crush, Kilo Code and Mistral Vibe, together with tmux terminal access and git staging/commits. Compare the specific harness and workflow you use.

**[OpenCode Mobile](https://opencode.ai/mobile)** and **[Claude Code Remote Control](https://code.claude.com/docs/en/remote-control)** are first-party ways to continue their respective sessions from a phone. They may be the right choice if that is the only harness you use. Togo is useful when you want OpenCode, OMP, Pi and other harnesses in one app with the same host connections and terminal access.

## Togo's current limits

- iPhone/iPad require iOS/iPadOS 26; the optional Watch companion requires watchOS 26. There is no Android, web or desktop client.
- Native chat and session controls require the helper, SSH and tmux on macOS or Linux. Plain terminal access works without the helper.
- Togo manages sessions on its own tmux socket. Sessions on the default socket can be listed and attached as terminals; they do not automatically become managed native chats.
- Agent controls and compatible versions vary by adapter. Some changes use terminal commands or a confirmed restart. An unsupported source falls back to the terminal.
- Herdr, task worktrees, Tern Surface Protocol and cross-agent handoff are on the [roadmap](https://github.com/gettogo/togo/discussions/1).

[Back to the README](../README.md)
