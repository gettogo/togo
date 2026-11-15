import Foundation

/// Content and state of a native list, separate from desktop layout hints.
public struct HarnessPickerPresentation: Codable, Hashable, Sendable {
    public struct Column: Codable, Hashable, Sendable {
        public var id: String
        public var format: String?
        public init(id: String, format: String? = nil) { self.id = id; self.format = format }
    }
    public var emptyText: String?
    public var searchPrompt: String?
    public var subtitle: String?
    public var noun: String?
    public var state: String?
    public var message: String?
    public var total: Int?
    public var columns: [Column]?

    public init(emptyText: String? = nil, searchPrompt: String? = nil, subtitle: String? = nil, noun: String? = nil,
                state: String? = nil, message: String? = nil, total: Int? = nil, columns: [Column]? = nil) {
        self.emptyText = emptyText; self.searchPrompt = searchPrompt; self.subtitle = subtitle; self.noun = noun
        self.state = state; self.message = message; self.total = total; self.columns = columns
    }
}

public struct HarnessPickerItemAppearance: Codable, Hashable, Sendable {
    public struct Badge: Codable, Hashable, Sendable {
        public var text: String
        public var tone: String?
        public init(text: String, tone: String? = nil) { self.text = text; self.tone = tone }
    }
    public struct Chip: Codable, Hashable, Sendable {
        public var text: String
        public var on: Bool?
        public var auto: Bool?
        public init(text: String, on: Bool? = nil, auto: Bool? = nil) { self.text = text; self.on = on; self.auto = auto }
    }
    public struct Mark: Codable, Hashable, Sendable {
        public var text: String?
        public var icon: String?
        public var seed: String?
        public init(text: String? = nil, icon: String? = nil, seed: String? = nil) { self.text = text; self.icon = icon; self.seed = seed }
    }
    public var badges: [Badge]?
    public var chips: [Chip]?
    public var mark: Mark?
    public var depth: Int?
    public var status: String?
    public var tone: String?
    public var icon: String?
    public var nodeKind: String?

    public init(badges: [Badge]? = nil, chips: [Chip]? = nil, mark: Mark? = nil, depth: Int? = nil,
                status: String? = nil, tone: String? = nil, icon: String? = nil, nodeKind: String? = nil) {
        self.badges = badges; self.chips = chips; self.mark = mark; self.depth = depth
        self.status = status; self.tone = tone; self.icon = icon; self.nodeKind = nodeKind
    }
}
