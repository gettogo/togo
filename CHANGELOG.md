# Changelog

The helper and the app are versioned separately. A newer helper always works with an older app.

## Helper

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
