import Foundation
import SwiftUI
import TogoKit
import UIKit

/// Where a link tapped in the chat goes.
enum ChatLinkRoute: Equatable {
    /// A web page: Safari's view inside the app.
    case web(URL)
    /// A file in the session's folder: its diff when the agent changed it, else the file.
    case file(path: String, line: Int?, changed: Bool)
    /// A mail link: the system's mail compose.
    case mail(URL)
    /// Any other scheme (`shortcuts:`, `tel:`, another app's): never opened from a model's
    /// text, as in the terminal (`LinkPolicy`).
    case refused
}

/// Makes the chat's links real. Markdown links stay as they are; bare URLs are found with the
/// system's data detector; file references (`web/LICENSE:1`, `src/app.ts`, `@path`) become
/// `togo-file` links the router opens in the review. Never inside fenced code text unless
/// asked (`plain(_:)` for code wells).
enum ChatLinks {
    static let fileScheme = "togo-file"

    // MARK: Building

    /// Inline markdown with every link the text holds. Cached: a view body asks for the same
    /// block on every update of the reply around it.
    static func markdown(_ text: String) -> AttributedString {
        ChatTextCache.shared.value("m", text) {
            let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
            let parsed = (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
            return linked(parsed)
        }
    }

    /// Adds links to an attributed string: a code span that is a file reference as a whole,
    /// and in prose bare URLs and clear file references. Runs that already link keep theirs.
    static func linked(_ input: AttributedString) -> AttributedString {
        var out = input
        var found: [(Range<AttributedString.Index>, URL)] = []
        for run in out.runs where run.link == nil {
            let text = String(out[run.range].characters)
            if run.inlinePresentationIntent?.contains(.code) ?? false {
                if let ref = FileRef(code: text) { found.append((run.range, ref.url)) }
                continue
            }
            for (range, url) in detect(text) {
                let lower = out.characters.index(run.range.lowerBound, offsetBy: text.distance(from: text.startIndex, to: range.lowerBound))
                let upper = out.characters.index(lower, offsetBy: text.distance(from: range.lowerBound, to: range.upperBound))
                found.append((lower..<upper, url))
            }
        }
        for (range, url) in found {
            out[range].link = url
        }
        return styled(out)
    }

    /// Plain machine text (a code well, a command) with its URLs and file references linked.
    static func plain(_ text: String) -> AttributedString {
        ChatTextCache.shared.value("p", text) { plainUncached(text) }
    }

    private static func plainUncached(_ text: String) -> AttributedString {
        var out = AttributedString(text)
        for (range, url) in detect(text) {
            let lower = out.characters.index(out.startIndex, offsetBy: text.distance(from: text.startIndex, to: range.lowerBound))
            let upper = out.characters.index(lower, offsetBy: text.distance(from: range.lowerBound, to: range.upperBound))
            out[lower..<upper].link = url
        }
        return styled(out)
    }

    /// A prompt's words with its `@path` mentions linked.
    static func prompt(_ text: String) -> AttributedString {
        var out = AttributedString(text)
        let ns = text as NSString
        for match in mention.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            guard let whole = Range(match.range, in: text), let pathRange = Range(match.range(at: 1), in: text) else { continue }
            let path = trimmed(String(text[pathRange]))
            guard !path.isEmpty else { continue }
            let lower = out.characters.index(out.startIndex, offsetBy: text.distance(from: text.startIndex, to: whole.lowerBound))
            let upper = out.characters.index(lower, offsetBy: 1 + path.count)
            out[lower..<upper].link = fileURL(path: path, line: nil)
        }
        return styled(out)
    }

    /// Links are underlined in the text's own colour: the beacon only ever means needs-you.
    private static func styled(_ input: AttributedString) -> AttributedString {
        var out = input
        for run in out.runs where run.link != nil {
            out[run.range].underlineStyle = .single
        }
        return out
    }

    /// URLs, then file references outside them, in prose.
    static func detect(_ text: String) -> [(Range<String.Index>, URL)] {
        var found: [(Range<String.Index>, URL)] = []
        let ns = text as NSString
        let whole = NSRange(location: 0, length: ns.length)
        for match in detector?.matches(in: text, range: whole) ?? [] {
            guard let url = match.url, let range = Range(match.range, in: text),
                  ["http", "https", "mailto"].contains(url.scheme?.lowercased() ?? "") else { continue }
            found.append((range, url))
        }
        for match in pathPattern.matches(in: text, range: whole) {
            guard var range = Range(match.range(at: 1), in: text),
                  !found.contains(where: { $0.0.overlaps(range) }) else { continue }
            // A sentence's full stop or comma is not part of the path.
            while let last = text[range].last, ".,;:)".contains(last) {
                range = range.lowerBound..<text.index(before: range.upperBound)
            }
            guard let ref = FileRef(prose: String(text[range])) else { continue }
            found.append((range, ref.url))
        }
        return found.sorted { $0.0.lowerBound < $1.0.lowerBound }
    }

    // MARK: URLs

    static func fileURL(path: String, line: Int?, changed: Bool = false) -> URL {
        var components = URLComponents()
        components.scheme = fileScheme
        components.host = "open"
        var query = [URLQueryItem(name: "path", value: path)]
        if let line { query.append(URLQueryItem(name: "line", value: String(line))) }
        if changed { query.append(URLQueryItem(name: "changed", value: "1")) }
        components.queryItems = query
        return components.url ?? URL(string: "\(fileScheme)://open")!
    }

    static func route(_ url: URL) -> ChatLinkRoute {
        switch url.scheme?.lowercased() {
        case "http", "https":
            return .web(url)
        case fileScheme:
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let path = items.first { $0.name == "path" }?.value ?? ""
            let line = items.first { $0.name == "line" }?.value.flatMap(Int.init)
            let changed = items.first { $0.name == "changed" }?.value == "1"
            return path.isEmpty ? .refused : .file(path: path, line: line, changed: changed)
        case "mailto":
            return .mail(url)
        default:
            return .refused
        }
    }

    // MARK: Paths

    /// `path` relative to the session's folder when it lies inside it; as written otherwise.
    static func sessionPath(_ path: String, cwd: String) -> String {
        var path = path
        while path.hasPrefix("./") { path.removeFirst(2) }
        let root = cwd.hasSuffix("/") ? String(cwd.dropLast()) : cwd
        if !root.isEmpty, path.hasPrefix(root + "/") { return String(path.dropFirst(root.count + 1)) }
        return path
    }

    /// The changed file a reference names, as git names it: an exact match, or one path ending
    /// with the other.
    static func changedPath(_ path: String, cwd: String, changes: [String]) -> String? {
        let relative = sessionPath(path, cwd: cwd)
        return changes.first { change in
            change == relative || change.hasSuffix("/" + relative) || relative.hasSuffix("/" + change)
                || path.hasSuffix("/" + change)
        }
    }

    // MARK: Patterns

    private static let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue)

    /// A path-like word: optional `~`, `.` or `..` prefix, segments, an optional `:line[:col]`.
    private static let pathPattern = try! NSRegularExpression(
        pattern: #"(?<![\w@/.~:-])((?:~|\.{1,2})?/?(?:[\w.-]+/)*[\w-][\w.-]*(?::\d+){0,2})(?![\w/])"#
    )

    private static let mention = try! NSRegularExpression(pattern: #"(?<![\w])@((?:[\w.~-]+/)*[\w.-]+)"#)

    private static func trimmed(_ path: String) -> String {
        var path = path
        while let last = path.last, ".,;:)".contains(last) { path.removeLast() }
        return path
    }
}

/// Attributed text already built for a string, so drawing a reply again (a streamed update, a
/// row scrolled back into view) skips the markdown parse and the link detection. NSCache is
/// thread-safe and gives way under memory pressure; a memory warning empties it.
final class ChatTextCache: @unchecked Sendable {
    static let shared = ChatTextCache()

    private final class Box {
        let value: AttributedString
        init(_ value: AttributedString) { self.value = value }
    }

    private let cache = NSCache<NSString, Box>()
    private var observer: NSObjectProtocol?

    init() {
        cache.countLimit = 600
        // Cost is the text's UTF-16 length; about 8 MB of source text at most.
        cache.totalCostLimit = 4 << 20
        observer = NotificationCenter.default.addObserver(
            forName: UIApplication.didReceiveMemoryWarningNotification, object: nil, queue: nil
        ) { [weak self] _ in self?.purge() }
    }

    func purge() {
        cache.removeAllObjects()
        ChatMarkdown.cache.removeAll()
    }

    func value(_ kind: String, _ text: String, make: () -> AttributedString) -> AttributedString {
        let key = (kind + "\u{0}" + text) as NSString
        if let box = cache.object(forKey: key) { return box.value }
        let value = make()
        cache.setObject(Box(value), forKey: key, cost: key.length)
        return value
    }
}

/// A file reference found in text: its path and line.
struct FileRef: Equatable {
    var path: String
    var line: Int?

    var url: URL { ChatLinks.fileURL(path: path, line: line) }

    /// Extensions a code span alone may name as a file (`README.md`), so `user.name` is not one.
    static let fileExtensions: Set<String> = [
        "swift", "ts", "tsx", "js", "jsx", "mjs", "cjs", "go", "py", "rb", "rs", "md", "mdx", "json", "yaml", "yml",
        "toml", "txt", "html", "css", "scss", "sh", "zsh", "c", "h", "cc", "cpp", "hpp", "m", "mm", "java", "kt",
        "lock", "xml", "sql", "env", "cfg", "ini", "mod", "sum", "plist", "xcstrings", "gradle", "vue", "svelte",
        "php", "cs", "ex", "exs", "lua", "dart", "proto", "graphql", "csv", "log", "conf", "pbxproj", "xcconfig",
    ]

    /// Splits `path:12` or `path:12:3`.
    init?(raw: String) {
        guard !raw.isEmpty, !raw.contains("://"), !raw.contains(where: \.isWhitespace) else { return nil }
        var parts = raw.split(separator: ":", omittingEmptySubsequences: false).map(String.init)
        var line: Int?
        if parts.count >= 2, let n = Int(parts[1]) {
            line = n
            parts = [parts[0]]
        }
        guard parts.count == 1, let path = parts.first, !path.isEmpty, path.contains(where: \.isLetter) else { return nil }
        self.path = path
        self.line = line
    }

    /// A whole code span: any path with a folder, a line, or a known file extension.
    init?(code: String) {
        guard let ref = FileRef(raw: code.trimmingCharacters(in: .whitespaces)) else { return nil }
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "/._-~"))
        guard ref.path.unicodeScalars.allSatisfy(allowed.contains) else { return nil }
        guard ref.hasFolder || ref.line != nil || ref.knownExtension else { return nil }
        self = ref
    }

    /// A word in prose: only when it is clearly a file, so "and/or" and "Node.js" stay words.
    init?(prose: String) {
        guard let ref = FileRef(raw: prose) else { return nil }
        let anchored = ref.path.hasPrefix("/") || ref.path.hasPrefix("./") || ref.path.hasPrefix("../") || ref.path.hasPrefix("~/")
        let ok = (ref.line != nil && (ref.hasFolder || ref.hasExtension))
            || (ref.hasFolder && (ref.hasExtension || anchored))
        guard ok else { return nil }
        self = ref
    }

    var hasFolder: Bool { path.dropFirst().contains("/") || (path.hasPrefix("/") && path.count > 1) }

    var fileExtension: String? {
        let name = path.split(separator: "/").last.map(String.init) ?? path
        guard let dot = name.lastIndex(of: "."), dot != name.startIndex else { return nil }
        let ext = name[name.index(after: dot)...]
        guard let first = ext.first, first.isLetter, ext.count <= 10, ext.allSatisfy({ $0.isLetter || $0.isNumber }) else { return nil }
        return String(ext)
    }

    var hasExtension: Bool { fileExtension != nil }

    var knownExtension: Bool { fileExtension.map { Self.fileExtensions.contains($0.lowercased()) } ?? false }
}

/// A row's whole text in a sheet, for VoiceOver's "Select text" action and a reader who wants
/// the words on their own: any part can be selected there as in the chat.
///
/// A reply's markdown shows rendered (`markdown`), so a selected part copies as the words with
/// their formatting and never with `**` or backticks; code shows in SF Mono, exactly.
struct SelectTextSheet: View {
    @Environment(\.dismiss) private var dismiss
    let text: String
    var markdown = false
    var monospaced = false

    var body: some View {
        NavigationStack {
            SelectableText(
                key: [text, "\(markdown)", "\(monospaced)"], layout: .scrolls,
                insets: UIEdgeInsets(top: 16, left: 12, bottom: 16, right: 12),
                copy: { [markdown] in markdown ? ChatPasteboard.copy(selection: $0) : ChatPasteboard.copy(exact: $0.string) }
            ) { [text, markdown, monospaced] traits in
                if markdown {
                    let style = RichMarkdown.Style(
                        body: .preferredFont(forTextStyle: .body, compatibleWith: traits), color: .togoSnow
                    )
                    return RichMarkdown.attributed(text, style: style)
                }
                let style = monospaced
                    ? ChatSelectable.Style(textStyle: .body, monospaced: true)
                    : ChatSelectable.Style.prompt
                return ChatSelectable.attributed(ChatLinks.plain(text), style: style, traits: traits)
            }
            .background(Color.togoInk)
            .navigationTitle(ChatCopy.selectText)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(ChatCopy.done, systemImage: "checkmark") { dismiss() }
                        .keyboardShortcut(.cancelAction)
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

extension AppStore {
    /// Opens a link tapped in a tab's chat: web pages in Safari's view, files in the review
    /// (the diff when the agent changed the file), mail through the system; any other scheme
    /// does nothing.
    func openChatLink(_ url: URL, from tab: TabID) -> OpenURLAction.Result {
        switch ChatLinks.route(url) {
        case .web(let page):
            LinkCenter.shared.open(page, from: LinkCenter.Origin(hostID: tab.hostID, sessionID: tab.sessionID))
            return .handled
        case .file(let path, _, let changed):
            return openChatFile(path, changed: changed, from: tab) ? .handled : .discarded
        case .mail:
            return .systemAction
        case .refused:
            return .discarded
        }
    }

    /// The file on its own, so Close goes back to the chat: its diff when the session changed
    /// it, the file itself otherwise; "All changes" opens the rest of the review from there.
    /// False when the session is not listed (no folder to read from).
    private func openChatFile(_ path: String, changed: Bool, from tab: TabID) -> Bool {
        guard let row = controlCenter.machines.first(where: { $0.id == tab.hostID })?.sessions
            .first(where: { $0.id == tab.sessionID }) else { return false }
        let changes = harness(for: tab).map(Self.changedFiles) ?? []
        let diff = ChatLinks.changedPath(path, cwd: row.cwd, changes: changes)
        let route: ReviewRoute
        let reviewTab: ReviewTab
        if let diff {
            route = .diff(.worktree(path: diff))
            reviewTab = .changes
        } else if changed {
            // Changed, but not in the list the helper reports: the list itself.
            review(row, tab: .changes)
            return true
        } else {
            route = .file(path: ChatLinks.sessionPath(path, cwd: row.cwd), ref: nil, base: .session)
            reviewTab = .files
        }
        let capabilities = machine(for: tab.hostID)?.capabilities ?? []
        let subject = ReviewSubject(
            hostID: row.hostID, sessionID: row.id, bib: row.bib, title: row.name, cwd: row.cwd,
            managed: row.managed, capabilities: capabilities
        )
        openReview = ReviewSession(
            subject: subject, services: reviewServices, tab: reviewTab, machine: machineName(for: tab.hostID),
            navigation: ReviewNavigation(tab: reviewTab, focus: route)
        )
        review(row, tab: reviewTab)
        return true
    }

    /// The files mission control lists as changed, as git names them.
    private static func changedFiles(_ store: HarnessSessionStore) -> [String] {
        let rows = store.mission.mission?.sections.first { $0.kind == .changes }?.rows ?? []
        return rows.map { row in row.open.flatMap { $0.kind == .diff ? $0.ref : nil } ?? row.id }
    }
}
