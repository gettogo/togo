import Foundation
import TogoKit
import TogoTerminal
import UIKit

/// The demo's terminal: the app's own terminal view, drawn from what the session has done so
/// far instead of from a connection. It is live from the start, takes no typing (the keyboard
/// never comes up) and draws again whenever the script moves on or the view changes size.
@MainActor final class DemoTerminal {
    let engine: SwiftTermEngine
    let controller: ThemedTerminalController
    let handle: TerminalHandle
    private let screen: (_ cols: Int) -> String
    private var drawn: String?
    private let stop = StopAction()
    /// Called once the tab lets go of this terminal.
    var onStopped: () -> Void {
        get { stop.run }
        set { stop.run = newValue }
    }

    init(settings: AppSettings, screen: @escaping (_ cols: Int) -> String) {
        self.screen = screen
        engine = SwiftTermEngine(settings: LiveTerminal.terminalSettings(settings), theme: BuiltInThemes.all[0])
        engine.view.accessibilityLabel = WorkspaceCopy.terminal
        engine.accessibilityWords = AccessibilityWords(cursorLine: WorkspaceCopy.cursorLine, blank: WorkspaceCopy.blankLine)
        engine.acceptsFocus = false
        controller = ThemedTerminalController(
            terminal: TerminalViewController(engine: engine, gestureConfig: LiveTerminal.gestureConfig(settings.gestures)),
            choice: LiveTerminal.themeChoice(settings)
        )
        let (phases, sink) = AsyncStream.makeStream(of: LinkPhase.self)
        sink.yield(.live(transport: Self.transport, note: nil))
        let engine = self.engine
        let stop = self.stop
        handle = TerminalHandle(
            engineView: controller, phases: phases,
            search: TerminalSearch(
                find: { engine.find($0, caseSensitive: false, regex: false, backwards: $1) },
                clear: { engine.clearFind() }
            ),
            onStop: {
                sink.finish()
                stop.run()
            }
        )
        handle.screenRows = { [weak engine] in engine?.screenRows() }
        handle.links = { [weak engine] in engine?.linksOnScreen() ?? [] }
        engine.onResize = { [weak self] _ in self?.redraw(force: true) }
        redraw()
    }

    static let transport = "Demo"

    /// Draws the screen again if it changed.
    func redraw(force: Bool = false) {
        let cols = engine.size.cols > 0 ? engine.size.cols : 60
        let text = screen(cols)
        guard force || text != drawn else { return }
        drawn = text
        // Home, clear the screen and the scrollback, then the whole picture.
        engine.feed(ArraySlice(Array(("\u{1B}[H\u{1B}[2J\u{1B}[3J" + text).utf8)))
    }
}

/// The handle is made before its terminal is whole, so what stopping does is set afterwards.
@MainActor private final class StopAction {
    var run: () -> Void = {}
}

/// What the demo's terminals show, as plain text with ANSI colour.
enum DemoScreen {
    private static let dim = "\u{1B}[2m", bold = "\u{1B}[1m", italic = "\u{1B}[3m", reset = "\u{1B}[0m"
    private static let green = "\u{1B}[32m", yellow = "\u{1B}[33m", cyan = "\u{1B}[36m", red = "\u{1B}[31m"

    /// The agent's own screen: the conversation as its terminal UI lays it out, then its input.
    static func agent(items: [HarnessItem], prompt: String, phase: DemoScript.Phase, cols: Int) -> String {
        var lines = ["\(bold)OpenCode\(reset)  \(dim)~/status\(reset)", ""]
        for item in items where item.parent == nil {
            switch item.body {
            case .user(let user):
                lines += wrap(user.text, cols, lead: "\(cyan)┃\(reset) ", style: bold)
                lines.append("")
            case .thinking(let text):
                lines += wrap(text.text.trimmingCharacters(in: .whitespacesAndNewlines), cols, lead: "  ", style: dim + italic)
            case .assistant(let text):
                lines += wrap(text.text.trimmingCharacters(in: .whitespacesAndNewlines), cols, lead: "  ", style: "")
                lines.append("")
            case .tool(let tool):
                let mark = tool.exitCode.map { $0 == 0 } ?? true ? "\(green)✓\(reset)" : "\(red)✗\(reset)"
                let title = tool.title.isEmpty ? tool.name : tool.title
                lines += wrap(title, cols, lead: "  \(mark) ", style: "")
            case .question(let question):
                for ask in question.questions {
                    let answered = question.answered?[ask.id]?.first
                    let choice = ask.options.first { $0.id == answered }?.label
                    lines += wrap(ask.prompt, cols, lead: "  \(yellow)△\(reset) ", style: "")
                    lines.append("    " + dim + (choice ?? ask.options.map(\.label).joined(separator: "  ·  ")) + reset)
                }
            case .turn(let turn):
                let seconds = turn.endedAt.map { Int($0.date.timeIntervalSince(turn.startedAt.date)) }
                if let seconds {
                    lines.append("  \(dim)▣ Build · \(seconds / 60)m \(seconds % 60)s\(reset)")
                    lines.append("")
                }
            default:
                break
            }
        }
        let field: String = switch phase {
        case .ready: prompt.isEmpty ? "" : "\(dim)\(prompt.prefix(max(cols - 4, 8)))…\(reset)"
        case .working: "\(dim)working…\(reset)"
        case .waiting: "\(yellow)Permission required\(reset)"
        case .done: ""
        }
        lines.append("\(cyan)┃\(reset) > " + field)
        return lines.joined(separator: "\r\n")
    }

    /// Every item a recording leaves in the chat, the latest version of each, in order: what
    /// the agent's screen shows once the recording has played.
    static func items(in script: ReplayScript) -> [HarnessItem] {
        let decoder = JSONDecoder()
        var latest: [String: HarnessItem] = [:]
        for note in script.chapters.flatMap(\.notes) where note.method == "harness" {
            guard let notification = try? decoder.decode(HarnessNotification.self, from: note.params) else { continue }
            for item in notification.items ?? [] where (latest[item.id]?.seq ?? .min) < item.seq {
                latest[item.id] = item
            }
        }
        return latest.values.sorted { $0.seq < $1.seq }
    }

    /// A plain shell in the same folder.
    static func shell(written: Set<String>, project: DemoProject?) -> String {
        let prompt = "\(green)jamie@demo\(reset) \(bold)status\(reset) % "
        var lines = [prompt + "git status --short"]
        for file in project?.status ?? [] where written.contains(file.path) {
            let letters = file.index == "?" ? "??" : " " + file.worktree
            lines.append("\(file.index == "?" ? red : green)\(letters)\(reset) \(file.path)")
        }
        lines.append(prompt)
        return lines.joined(separator: "\r\n")
    }

    /// Wraps text to the terminal's width; every line starts with `lead`, which counts as
    /// `visibleLead` columns.
    static func wrap(_ text: String, _ cols: Int, lead: String, style: String) -> [String] {
        let visible = lead.replacingOccurrences(of: "\u{1B}\\[[0-9;]*m", with: "", options: .regularExpression).count
        let width = max(cols - visible - 1, 12)
        var out: [String] = []
        for paragraph in text.components(separatedBy: "\n") {
            var line = ""
            for word in paragraph.split(separator: " ", omittingEmptySubsequences: false) {
                var word = String(word)
                while word.count > width {
                    if !line.isEmpty { out.append(line); line = "" }
                    out.append(String(word.prefix(width)))
                    word = String(word.dropFirst(width))
                }
                if line.isEmpty {
                    line = word
                } else if line.count + 1 + word.count <= width {
                    line += " " + word
                } else {
                    out.append(line)
                    line = word
                }
            }
            out.append(line)
        }
        return out.map { lead + style + $0 + reset }
    }
}
