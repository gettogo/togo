import Foundation
import Observation
import SwiftUI
import TogoTerminal

/// A place a new session could start: an agent in a folder on a machine.
struct StartSuggestion: Identifiable, Equatable, Hashable, Sendable {
    var id: String
    var agentName: String
    var folder: String
    var machineName: String
}

/// What the prompt bar holds. The owner sets `mode` from the store; the bar answers through the
/// callbacks.
@Observable @MainActor final class PromptBarModel {
    enum Mode: Equatable {
        /// The oldest request that waits, with its place in the queue.
        case request(PromptRequest, position: Int, count: Int)
        /// The user is typing a reply to the request.
        case replying(PromptRequest)
        /// A terminal is open; the bar is its input line.
        case input(TabID)
        /// Nothing waits and no terminal is open: the bar starts a session.
        case start
        case hidden
    }

    /// What the bar draws, with the start line's own open or closed state folded in.
    enum Stage: Equatable {
        case request(PromptRequest, position: Int, count: Int)
        case replying(PromptRequest, position: Int, count: Int)
        case input(TabID)
        case start(expanded: Bool)
        case hidden

        /// The request on show, in either of its two forms.
        var request: PromptRequest? {
            switch self {
            case let .request(request, _, _), let .replying(request, _, _): request
            default: nil
            }
        }
    }

    /// How the bar moves from one stage to the next.
    enum Change: Equatable {
        /// Nothing the user would see as a new state, such as the queue count moving.
        case none
        /// The content changes in place and the bar's height follows.
        case resize
        /// The old content settles down and out, then the new rises in on `role`.
        case rise(MotionRole)
    }

    var mode: Mode = .hidden {
        didSet { modeChanged(from: oldValue) }
    }
    var startQuery = ""
    var startSuggestions: [StartSuggestion] = []

    var onAnswer: (PromptRequest, PromptAnswer) -> Void = { _, _ in }
    var onSend: (TabID, String) -> Void = { _, _ in }
    var onStart: (StartSuggestion) -> Void = { _ in }
    var onOpenStartSheet: () -> Void = {}
    /// Opens the session a request comes from, from its context line.
    var onOpen: (PromptRequest) -> Void = { _ in }

    /// The open terminal's name, for the input line's placeholder ("Message api").
    var tabName: (TabID) -> String = { $0.sessionID }
    /// The keys under the input line. Only keys that send something are drawn; latches, the
    /// pad, compose and dictation belong to the terminal's own key bar.
    var keyBar: KeyBarConfig = .standard
    /// Called when a key under the input line is tapped.
    var onKey: (TabID, KeyBarItem) -> Void = { _, _ in }
    /// Set by the owner while a text field outside the bar holds the keyboard, such as the
    /// terminal, so typing y, n or r there is not taken as an answer.
    var otherTextFocused = false
    /// Says a new request to VoiceOver without moving focus. Nil posts the system
    /// announcement; tests pass their own.
    @ObservationIgnored var announce: ((String) -> Void)?

    /// Whether the start line is open into its field and suggestions.
    private(set) var startExpanded = false
    var replyDraft = ""
    /// What is typed for the terminal on show. Each terminal keeps its own while the bar
    /// shows something else.
    var inputDraft: String {
        get { inputTab.flatMap { inputDrafts[$0] } ?? "" }
        set { if let inputTab { inputDrafts[inputTab] = newValue } }
    }
    private var inputDrafts: [TabID: String] = [:]
    private var inputTab: TabID? {
        if case let .input(tab) = mode { tab } else { nil }
    }
    /// Goes up when the user puts away a reply whose request has gone, so the owner can show
    /// what comes next.
    private(set) var dismissals = 0
    /// How long a new request waits before its answers work, as system alerts do, so a tap or
    /// key meant for what was there before cannot answer it.
    static let defaultArmDelay: Duration = .milliseconds(500)
    private let armDelay: Duration
    /// The request whose answers work now.
    private var armedID: String?
    @ObservationIgnored private(set) var arming: Task<Void, Never>?
    /// Whether a field in the bar holds the keyboard.
    var barTextFocused = false
    /// Set by the owner while the terminal holds the keyboard: a request keeps to its smallest
    /// form so the terminal keeps most of the height.
    var compact = false
    /// Goes up each time a request arrives into a bar that was not already showing one; the bar
    /// plays the `connect` haptic on it.
    private(set) var arrivals = 0

    /// The queue place of the request being replied to, so the context line keeps "1 of 3".
    private var replyQueue = (position: 1, count: 1)
    /// Requests whose beacon has lit and whose command has typed out, so each happens once.
    @ObservationIgnored private var introduced: [Intro: Set<String>] = [:]

    enum Intro: Hashable {
        case beacon, command
    }

    init(mode: Mode = .hidden, armDelay: Duration = PromptBarModel.defaultArmDelay) {
        self.armDelay = armDelay
        self.mode = mode
        armedID = Self.request(in: mode)?.id
    }

    var stage: Stage {
        switch mode {
        case let .request(request, position, count): .request(request, position: position, count: count)
        case let .replying(request): .replying(request, position: replyQueue.position, count: replyQueue.count)
        case let .input(tab): .input(tab)
        case .start: .start(expanded: startExpanded)
        case .hidden: .hidden
        }
    }

    /// The user's choice (Settings › Terminal); off leaves only ⌘↩ and ⌘..
    var singleKeyAnswers = true

    /// Hardware y, n and r answer only while a request shows and no field holds the keyboard.
    var acceptsKeyAnswers: Bool {
        guard singleKeyAnswers, case let .request(request, _, _) = mode, isArmed(request) else { return false }
        return request.asksYesOrNo && !otherTextFocused && !barTextFocused
    }

    /// ⌘↩ and ⌘. answer the request in the bar even while a field holds the keyboard: with a
    /// modifier they cannot be typed by accident.
    var acceptsCommandAnswers: Bool {
        guard case let .request(request, _, _) = mode, isArmed(request) else { return false }
        return request.asksYesOrNo
    }

    /// Whether the request's answers work now: it is the one in the bar, it has been there
    /// long enough to be read, and no answer is on its way.
    func isArmed(_ request: PromptRequest) -> Bool {
        guard let current = stage.request, current.id == request.id, armedID == request.id else { return false }
        return current.answerable
    }

    /// "1 of 3" when more than one request waits, otherwise nil.
    static func queueText(position: Int, count: Int) -> String? {
        guard count > 1 else { return nil }
        return PromptCopy.queue(position: max(1, min(position, count)), count: count)
    }

    /// What VoiceOver says when a request arrives: "04 app asks: Run npm test?".
    static func announcement(for request: PromptRequest) -> String {
        PromptCopy.asks(bib: request.bib, name: request.sessionName, question: request.question)
    }

    /// Up to three suggestions whose agent, folder or machine holds every word of the query.
    var visibleSuggestions: [StartSuggestion] {
        Self.filter(startSuggestions, query: startQuery)
    }

    static func filter(_ suggestions: [StartSuggestion], query: String) -> [StartSuggestion] {
        let words = query.lowercased().split(whereSeparator: \.isWhitespace)
        let matching = suggestions.filter { suggestion in
            let haystack = "\(suggestion.agentName) \(suggestion.folder) \(suggestion.machineName)".lowercased()
            return words.allSatisfy { haystack.contains($0) }
        }
        return Array(matching.prefix(3))
    }

    static func change(from old: Stage, to new: Stage) -> Change {
        if old == new { return .none }
        let oldID = old.request?.id
        let newID = new.request?.id
        if oldID != newID {
            // A new request rises in; an answered one settles and the quiet line comes back.
            return .rise(newID == nil ? .collapse : .dropIn)
        }
        if newID != nil, case .request = old, case .request = new {
            // The same request with a new place in the queue.
            return .none
        }
        if case .hidden = old { return .rise(.dropIn) }
        return .resize
    }

    // MARK: Actions

    /// Answers the request with id `id`, the one that was on screen. Dropped when the bar holds
    /// another request by now, or this one has not been there long enough to be read.
    func answer(_ answer: PromptAnswer, to id: String) {
        guard let request = stage.request, request.id == id, isArmed(request) else { return }
        switch answer {
        case .yes, .no: guard request.asksYesOrNo else { return }
        case .option(let n): guard request.more.contains(where: { $0.id == n }) else { return }
        case .reply: guard request.allowsReply else { return }
        }
        onAnswer(request, answer)
    }

    func beginReply() {
        guard case let .request(request, position, count) = mode, request.allowsReply, request.answerable else { return }
        replyQueue = (position, count)
        mode = .replying(request)
    }

    /// Back to the answers, which wait out the arming delay again. A reply whose request has
    /// gone is put away, words and all.
    func cancelReply() {
        guard case let .replying(request) = mode else { return }
        replyDraft = ""
        if request.state == .withdrawn {
            dismissals += 1
            return
        }
        mode = .request(request, position: replyQueue.position, count: replyQueue.count)
        rearm(request.id)
    }

    func sendReply() {
        guard case let .replying(request) = mode, request.allowsReply, request.answerable else { return }
        let text = replyDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        replyDraft = ""
        // The reply is on its way: the bar goes back to the request, which shows it sending.
        mode = .request(request, position: replyQueue.position, count: replyQueue.count)
        onAnswer(request, .reply(text))
    }

    /// Sends the line as typed, leading spaces and all; only the trailing line break goes.
    func sendInput() {
        guard case let .input(tab) = mode else { return }
        let text = String(inputDraft.reversed().drop(while: \.isNewline).reversed())
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        inputDraft = ""
        onSend(tab, text)
    }

    func tapKey(_ item: KeyBarItem) {
        guard case let .input(tab) = mode else { return }
        onKey(tab, item)
    }

    /// A hardware key pressed in the input line that the terminal needs (Esc, ⌃C, Tab, ⇧Tab,
    /// arrows for history): sent to the session as its bytes. False leaves it to the field.
    func hardwareKey(_ key: PromptHardwareKey) -> Bool {
        guard case let .input(tab) = mode,
              let bytes = key.bytes(draftEmpty: inputDraft.isEmpty) else { return false }
        onKey(tab, KeyBarItem(title: key.title, action: .steps([.text(bytes)])))
        return true
    }

    /// The keys drawn under the input line: the ones made only of text, which the bar can send.
    var inputKeys: [KeyBarItem] {
        keyBar.items.filter { item in
            guard case .steps(let steps) = item.action, !steps.isEmpty else { return false }
            return steps.allSatisfy { if case .text = $0 { true } else { false } }
        }
    }

    func openStart() {
        guard case .start = mode else { return }
        startExpanded = true
    }

    func cancelStart() {
        startExpanded = false
        startQuery = ""
    }

    func pick(_ suggestion: StartSuggestion) {
        cancelStart()
        onStart(suggestion)
    }

    /// Return in the start field picks the first suggestion.
    func pickFirst() {
        guard let first = visibleSuggestions.first else { return }
        pick(first)
    }

    func openStartSheet() {
        cancelStart()
        onOpenStartSheet()
    }

    // MARK: Once per request

    /// Whether `part` of the request has yet to play its one entrance.
    func introPending(_ part: Intro, for id: String) -> Bool {
        !(introduced[part]?.contains(id) ?? false)
    }

    /// Marks `part` as played for the request; true the first time only.
    func takeIntro(_ part: Intro, for id: String) -> Bool {
        introduced[part, default: []].insert(id).inserted
    }

    private func modeChanged(from old: Mode) {
        if case .start = mode {} else if startExpanded {
            startExpanded = false
            startQuery = ""
        }
        let wasShowing = Self.request(in: old)
        let now = Self.request(in: mode)
        guard let now, now.id != wasShowing?.id else { return }
        rearm(now.id)
        replyDraft = ""
        if wasShowing == nil {
            arrivals += 1
        }
        let text = Self.announcement(for: now)
        if let announce {
            announce(text)
        } else {
            AccessibilityNotification.Announcement(text).post()
        }
    }

    /// Disarms the answers, then arms them for `id` after the delay if it is still on show.
    private func rearm(_ id: String) {
        armedID = nil
        arming?.cancel()
        let delay = armDelay
        arming = Task { @MainActor [weak self] in
            try? await Task.sleep(for: delay)
            guard let self, !Task.isCancelled, self.stage.request?.id == id else { return }
            self.armedID = id
        }
    }

    private static func request(in mode: Mode) -> PromptRequest? {
        switch mode {
        case let .request(request, _, _), let .replying(request): request
        default: nil
        }
    }
}

/// A key from a hardware keyboard in the prompt bar's input line, as the terminal would get it.
struct PromptHardwareKey: Equatable {
    enum Name: Equatable { case escape, tab, up, down, left, right, control(Character) }
    let name: Name
    var shift = false

    var title: String {
        switch name {
        case .escape: "Esc"
        case .tab: shift ? "Shift-Tab" : "Tab"
        case .up: "Up"
        case .down: "Down"
        case .left: "Left"
        case .right: "Right"
        case .control(let letter): "Ctrl-" + String(letter).uppercased()
        }
    }

    /// The bytes the terminal gets, or nil when the field keeps the key: left and right move
    /// the caret while there is text to move through.
    func bytes(draftEmpty: Bool) -> String? {
        switch name {
        case .escape: return "\u{1b}"
        case .tab: return shift ? "\u{1b}[Z" : "\t"
        case .up: return "\u{1b}[A"
        case .down: return "\u{1b}[B"
        case .right: return draftEmpty ? "\u{1b}[C" : nil
        case .left: return draftEmpty ? "\u{1b}[D" : nil
        case .control(let letter):
            guard let ascii = letter.lowercased().first?.asciiValue, (97...122).contains(ascii) else { return nil }
            return String(UnicodeScalar(ascii - 96))
        }
    }
}
