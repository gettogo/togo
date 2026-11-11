# Changelog

The helper and the app are versioned separately. A newer helper always works with an older app.

## Helper

### 1.0.1 (2026-11-11)

**Fixed**

- Swiping or dragging in the terminal of a full-screen agent, such as OpenCode, froze the screen until you pressed Esc. Swipes now reach the agent as arrow keys, and drags and double-taps no longer freeze it.
- A two-finger tap in the terminal could open tmux's pane menu, which offers Kill and Respawn. It no longer appears.
- A session whose folder was deleted or moved kept reconnecting. It now connects, and the helper never runs anything for it in a folder you didn't choose.
- After the helper updated itself, its background service could run outside launchd or systemd, so nothing restarted it after a crash until the next login. It now stays under the service manager.

### 1.0.0 (2026-11-10)

First release.

## App

### 1.0.0

First release.
