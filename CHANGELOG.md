# Changelog

The helper and the app are versioned separately. A newer helper always works with an older app.

## Helper

### 1.1.0 (2026-11-17)

**Added**

- Menus an agent draws in its terminal, such as model lists, settings, command lists and pickers, open as native lists in the app, with search where the agent offers it. They are read from the agent's screen, so no agent needs setting up.
- A one-line question from an agent, such as a key or a code, opens as a single field. What you enter isn't shown again.
- The command list in chat includes the agent's own commands, read from the agent.
- In OpenCode, terminal commands appear in the command palette and custom agents appear as modes. You can pick a model variant, connect or disconnect MCP servers, and see background shells in mission control. Undo and redo show in the chat.
- OpenCode forms support required, preset and conditional fields. Provider sign-in shows its address and code in the app, with a button that opens the address on your phone.
- OMP's own screens, such as its resume, model and settings choosers, forms and questions, open as native screens. Typing from the phone waits while one is open.
- In OMP, form questions, pictures in tool results and background jobs show in the app. Effort offers the levels the active model takes, and a setting the project overrides says so.
- The installer makes `togo` available in a new terminal. Where it can't link from `/usr/local/bin`, it adds one line to your zsh, bash or fish profile; `--no-modify-path` skips that.

**Changed**

- Mission control lists what is running and what went wrong. An agent or background shell that finishes leaves the list, and one that failed or was stopped stays until your next prompt.
- A command whose result can't be seen is reported as sent instead of done. If its list opens later, the app offers it.
- A newer release of an agent keeps its native chat while its major version is the same.
- File search follows the folder the agent is in and covers folders added to the session.

**Fixed**

- A session could be reported done, and shown idle, while subagents it had started were still running. It now stays working until the last one finishes.
- Subagents and background jobs could show as running after the agent had exited or the helper had restarted. They now show their real state.
- When an agent moved to another conversation, the chat mixed the old messages with the new ones. The chat now starts over on the new conversation.
- A prompt sent just as the agent moved to another conversation could reach the wrong one or be sent twice. It is now refused or reported, and never sent again.
- An approval answered from the phone could be reported before the agent had taken it. It is now reported once taken; otherwise the request stays open and says the answer wasn't confirmed.
- Two approvals asked back to back with the same wording could be mixed up, so an answer meant for the first reached the second. Each answer now reaches its own request.
- A request left waiting longer than ten minutes could no longer be answered from the phone. It now stays open for as long as the agent is asking.
- A tool finishing could close another tool's pending approval. Each approval now closes only with its own tool.
- A needs-you notification that reached no device was dropped. It is now sent again after 1, 3 and 9 minutes.
- An answer from a notification was lost when the helper couldn't read the terminal for a moment. It is now retried.
- Ordinary reply text, such as a ticket number or a link containing "login", could raise a sign-in request. It no longer does.
- A session whose agent was started again by hand in its pane stayed shown as ended. It now shows as running again.
- Chat stopped updating after a break in the agent's feed until the session was reopened. It now recovers on its own.
- A message starting with a slash word the agent doesn't know failed to send. It is now typed into the agent as text.
- Tool steps containing tabs or control characters were missing from the chat. They now show.
- Typing from the phone was lost when the session's terminal was scrolled back in tmux copy mode. It now reaches the agent.
- The Lock Screen activity could keep an old state after an update failed to send, or stay on "Offline" after the machine was back. It now catches up.
- Ports listed the same port once per worker process and per address, and cut command names short on macOS. Each port now shows once, with its full command name.
- Review showed renamed files, files with unusual names, and files with both staged and unstaged changes incorrectly. They now show as git reports them.
- Tool output could hide ordinary prose after the word "password" and show some real secrets written as lower-case NAME=value. Hiding now follows the shape of the secret.
- On Linux, restarting or updating the helper ended every session. Sessions now keep running, and the helper stays under systemd after a restart.
- On Linux, opening a link from an agent failed after the helper was moved or reinstalled. Links now open.
- `togo update` could replace a helper built from source with the older release. It now leaves a newer build alone.
- `togo uninstall` left its background service running. It now stops it.
- The installer failed on a Mac with neither minisign nor OpenSSL 3. It now installs there.
- In OpenCode, a prompt sent from the chat after /new or a fork in the terminal went to the conversation you had left. It now goes to the current one.
- In OpenCode, a chat opened after a helper restart showed no earlier messages. It now shows its history.
- In OpenCode 1.x and Kilo, a prompt sent while the agent worked skipped the queue. Queued prompts now go out in order.
- In OpenCode, a picture sent from the phone arrived as a file path. It now arrives as a picture.
- In OpenCode, the resume picker listed archived sessions and found only the newest 50. It now lists live sessions only and searches all of them.
- In OpenCode, one subagent could show as two rows. It now shows once.
- In OMP, the chat stopped updating once OMP started background agents. It now keeps updating, and each background agent shows once under its task name.
- In OMP, a session was reported done while OMP carried on working after one of its own reminders. It now stays working until that work ends.
- In OMP, the session title didn't follow a retitle, and a subagent's title could replace it. The title now follows the conversation.
- In OMP, no replies showed in the chat when OMP's data folder was moved or a profile was used. They now show.
- In OMP, a forked conversation's replies were missing or shown as a subagent's. They now show in the chat.
- In OMP, resuming from the app dropped the options the session was started with. Resume now keeps the model, folders and extensions.
- In OMP, the plan ignored a todo list edited by hand. It now follows the list.
- In Pi, a message sent while the agent worked ended the turn as interrupted, and its tool results never showed. The message now joins the running turn.
- In Pi, the chat and the resume list were empty for a folder whose name holds characters such as [ and ]. They now show.
- In Pi, resuming a session from the app could fail, and unnamed sessions had no title. Resume now works, and an unnamed session is titled by its first message.
- In Kimi, the old plan, subagents and background tasks stayed in mission control after /new. They are now cleared.
- In Kimi, a question or approval could stay open after its turn was cancelled, which also held back later done notifications. It now closes when the turn ends.
- In Kimi, a background task could show as running after it had finished. Its row now closes.
- In Kimi, a turn started by a skill showed the agent answering nothing. The chat now shows its prompt.
- In Kimi, a turn could end early while Kimi went on working in it. It now ends when Kimi does.
- In Goose and Crush, the chat kept the old conversation after Resume, or stayed empty after a restart, until the agent wrote to it. It now moves to the resumed conversation at once.
- In Goose and Crush, a new session could pick up an older conversation of another instance in the same folder. It now follows only a conversation made after it started.
- In Crush, a finished tool could go back to running and then show as interrupted. It now stays finished.
- In Cline, the chat stayed on the old conversation after Resume. It now follows the resumed one at once.
- In Cline, a turn that paused on a permission prompt or a long tool never showed as running again, and its later tools read as interrupted. The turn now reopens when it goes on.
- In Copilot, replies stopped showing after a very large tool result. They now keep showing.
- In Copilot, earlier replies were drawn again as part of the current turn after a helper restart. Each reply now shows once.
- In Droid, Copilot, Cursor and Amp, a failed turn stayed shown as running and its error never appeared. The turn now ends with the error.
- In Droid, an earlier turn's failure could be drawn on a turn that succeeded. Each turn now shows its own outcome.
- In Codex, an approval that offered a policy amendment among its choices never reached the phone. It now does.
- In Codex, the chat could follow the wrong conversation after /new or /resume in the terminal. It now follows the one the terminal is on.
- In Codex, Stop, Compact and model changes could target the wrong conversation or model after /model in the terminal. They now follow the session.
- Codex CLI 0.169 lost its native chat. It works natively again.
- In Claude Code, a stopped subagent ended the main turn as interrupted. Only stopping the main agent ends it now.
- In Claude Code, text, plan rows and file counts could double after the conversation was read again. They now show once.
- In Claude Code, a background agent or shell could stay shown as running after it had finished. Its row now settles.
- In Claude Code, the context meter jumped between two values on sessions with a larger context window. It now follows the agent's own figure.
- In Claude Code, a retry warning stayed in a turn that went on to complete. It now says the request went through.

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
- Disconnecting while a request waited for input could block RPC shutdown. The connection now closes before waiting requests are canceled.

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

### 1.1.0 (2026-11-17)

**Added**

- An agent's own menus, pickers, forms and questions open as native screens over the chat, with search where the agent offers it.
- Mermaid diagrams show as drawings in the chat. Tap one to zoom and pan, or switch to its source and copy it.
- A line above the message field says how many agents are running in the background. Tap it to see them.
- Links in an agent's terminal panels and sign-in screens open on your phone.
- While connecting, the app says which route it is trying and for how long. Diagnostics in Settings lists how long each route took.
- Pickers can search on the machine, so a conversation older than the newest 50 can be found.

**Changed**

- The line under "Working" always says what the agent is doing: the step that runs, then the last step it finished, and "Thinking" before the first.
- Mission control puts what is running first, counts it, and states each agent's and shell's condition in words.
- The Mission Control button opens mission control from the overflow menu too. Stop, Compact and Go to request are in the session menu on every layout.
- Take Back sits in the footer of a queued message.
- A machine with no live connection shows its sessions' status as last known.
- Messages about a missing or outdated tmux say what is wrong and how to fix it.

**Fixed**

- Closing a question's sheet cancelled the agent's question. The question now stays open until you answer it or choose Cancel Request.
- Tapping a request's notification or Lock Screen activity could open an empty screen. It now opens the session's chat at the request.
- The chat could scroll away when you sent a message. It now stays on the message you sent.
- The chat was rebuilt from the top when it reconnected after a helper restart. It now keeps the conversation on screen.
- A message could stay queued, and the chat keep showing "Working", after the agent had finished. Both now end with the turn.
- Connecting to a machine over a slow link could take minutes. Early attempts now follow each other without growing pauses, and a route that needs time to wake gets it.
- When an agent changed conversation, prompts held on the phone, pending model changes and late replies could land in the new conversation. They now stay with the conversation they were made for.
- An approval the terminal didn't take was shown as answered. It now stays open and says the answer wasn't confirmed.
- An Approve or Deny sent from a notification wasn't confirmed by the machine. It now is, and the phone says when an answer arrived too late or couldn't be applied.
- With more than one device paired, a device with Togo open silenced notifications on the others. Only the device with Togo open is now left out.
- Forms showed every field as required and ignored conditions and defaults. They now follow the agent's form.
- A saved password or key could be lost when replacing it failed. The old one is now kept.
- The app could ask for sign-in again after a brief connection drop. It now reconnects without asking.
- A request interrupted by a connection drop could be sent a second time. It is now reported as unconfirmed instead.
- Hardware Shift-Return sent different keys depending on the input mode. It now sends the same key in both.
- A terminal for a plain tmux session that had ended kept trying to attach. It now stops.
- Local pages an agent opens couldn't be reached from the phone. They now open through the session's port preview.
- At larger text sizes, the message field cut off its text and the Stop button. Both now scale with the text.
- Names and paths gained a hyphen when they wrapped. They now wrap without one.

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
