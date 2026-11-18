import Foundation

/// Rich content is a property of a complete fence, never of the harness that wrote it.
public struct MarkdownFence: Hashable, Sendable {
    public enum Renderer: String, Sendable { case mermaid }
    public let language: String
    public let source: String
    public let renderer: Renderer

    public init?(language: String?, source: String, closed: Bool, streaming: Bool = false) {
        guard let renderer = MarkdownFenceRegistry.renderer(language: language, source: source, closed: closed, streaming: streaming) else { return nil }
        self.language = language!.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        self.source = source
        self.renderer = renderer
    }
}

public enum MarkdownFenceRegistry {
    public static let sourceLimit = 16 * 1024
    private static let renderers: [String: MarkdownFence.Renderer] = ["mermaid": .mermaid]

    public static func renderer(language: String?, source: String, closed: Bool, streaming: Bool) -> MarkdownFence.Renderer? {
        guard closed, !streaming, !source.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              source.utf8.count <= sourceLimit,
              let language = language?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased(),
              let renderer = renderers[language] else { return nil }
        // Diagram configuration is application-owned. Directives/frontmatter remain source.
        guard !source.contains("%%{"), !source.trimmingCharacters(in: .whitespacesAndNewlines).hasPrefix("---") else { return nil }
        return renderer
    }

    public static func block(language: String?, source: String, closed: Bool) -> MarkdownBlocks.Block {
        if let fence = MarkdownFence(language: language, source: source, closed: closed) { return .richCode(fence) }
        return .code(language: language, text: source)
    }
}

/// Both markdown readers use the same closure rule; an opening tag never closes a fence.
public struct MarkdownFenceMarker: Sendable {
    public let marker: Character
    public let count: Int
    public let language: String?

    public static func opening(_ line: String) -> Self? {
        let line = line.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let marker = line.first, marker == "`" || marker == "~" else { return nil }
        let count = line.prefix { $0 == marker }.count
        guard count >= 3 else { return nil }
        let tag = line.dropFirst(count).trimmingCharacters(in: .whitespacesAndNewlines)
        if marker == "`", tag.contains("`") { return nil }
        return Self(marker: marker, count: count, language: tag.isEmpty ? nil : tag)
    }

    public func closes(_ line: String) -> Bool {
        let line = line.trimmingCharacters(in: .whitespacesAndNewlines)
        return line.count >= count && line.allSatisfy { $0 == marker }
    }
}
