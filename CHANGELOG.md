# Changelog

The helper and the app are versioned separately. A newer helper always works with an older app.

## Helper

### 1.0.4 (2026-11-13)

**Fixed**

- New agent sessions could reopen an old conversation. They now start fresh unless you choose a conversation to resume.
- Resumed conversations could replay completed work as new activity. History now loads as one snapshot, with a loading signal before replay begins.
- Earlier chat messages could be skipped when reconnecting. The helper now identifies missing history so the app can retrieve it.
- Live API-backed sessions could lose their connection after a helper restart, including sessions started through symlinked folders. Their endpoint and credentials now recover together.
- Agent updates could lose names, roles and clocks, and current Codex worker activity could go missing. Those details now survive updates and replay.
- Approval requests could arrive as a Live Activity without their question or actions. A normal actionable notification now remains available until the device confirms it saved the request.
- Restart could report failure after the service manager had already replaced the daemon. It now verifies the replacement against the original shutdown request.
- Removing and re-adding a paired relay could interrupt withdrawal of its offline notification. The old connection now drains before its replacement opens.

### 1.0.3

**Fixed**

- A session's chat could switch to a conversation of the same agent started outside Togo in the same folder, along with its working state and alerts. This affected OMP, Pi, Kimi, Goose, Crush, Vibe and Cline. Each session now follows only its own agent.
- A Codex started outside Togo in the same folder could send its approvals and alerts to a Togo Codex session. They are now ignored.
- OMP's todo list didn't show in the chat or mission control. It shows again, grouped by phase.
- In OMP, /resume and /new in the terminal didn't move the chat to the new conversation. The chat now follows within a couple of seconds.
- After the helper restarted, an open chat could show "Nothing said yet" until you left and came back. It now shows the whole conversation.

**Changed**

- A todo list the helper can't read shows as a tool step in the chat instead of disappearing.
- In Goose and Crush, a conversation you start inside the agent shows in the chat once the session restarts. This keeps another Goose or Crush in the same folder out of the chat.

### 1.0.2

**Changed**

- `togo update` updates Homebrew installs by running `brew upgrade` for you, and `togo update --check` works for them.

### 1.0.1 (2026-11-11)

**Fixed**

- Swiping or dragging in the terminal of a full-screen agent, such as OpenCode, froze the screen until you pressed Esc. Swipes now reach the agent as arrow keys, and drags and double-taps no longer freeze it.
- A two-finger tap in the terminal could open tmux's pane menu, which offers Kill and Respawn. It no longer appears.
- A session whose folder was deleted or moved kept reconnecting. It now connects, and the helper never runs anything for it in a folder you didn't choose.
- After the helper updated itself, its background service could run outside launchd or systemd, so nothing restarted it after a crash until the next login. It now stays under the service manager.

### 1.0.0 (2026-11-10)

First release.

## App

### 1.0.2 (2026-11-13)

**Fixed**

- Typing or pasting after Send could lose new text, and late dictation could restore words already sent. New input stays intact and sent words stay cleared.
- Option text input differed between terminal encoders, and review shortcuts could act on the wrong pane. Both now follow the selected input mode and focused pane.
- Chat could skip earlier messages or show no progress while a saved conversation loaded. It now retrieves missing history and shows progress while keeping the current messages visible.
- Approval notifications and Live Activities could omit the question or actions, or give no answer feedback. They now show the request and report answer delivery.
- A temporary credential interruption could incorrectly require signing in again. Connections now retry without a manual refresh.
- Agent rows could be blank and status values could misalign when they wrapped. Labels stay readable and status columns remain aligned at larger text sizes.
- Large terminal output bursts could stall rendering. Output now leaves time to keep the screen responsive.
- An agent's refusal or busy response could be mistaken for success. The app now reports it consistently.

**Added**

- A notice appears in the session list when a newer compatible app version is available in the App Store.

### 1.0.1

**Added**

- Paste a private key when adding a machine. Togo clears it from the clipboard after import.
- Update the helper from the Control Center when a new version is out, including Homebrew installs.

**Changed**

- The checkmark on the machine form saves and closes. You only see another screen if Togo needs an answer, like trusting a new host key.

**Fixed**

- A helper installed after the machine was added wasn't detected.

### 1.0.0

First release.
