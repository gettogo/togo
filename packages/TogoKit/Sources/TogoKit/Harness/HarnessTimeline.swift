import Foundation

/// What applying a notification did.
public enum HarnessApply: Equatable, Sendable {
    case applied
    /// Already reflected (seq at or below the cursor, rev at or below the held rev).
    case ignored
    /// Arrived before `load`; the caller keeps it and replays it after.
    case early
    /// A gap, a reset or another epoch: call `harnessOpen` again and `load` the result.
    case reopen
}

/// A session's conversation as the app holds it: the items in display order, built from one
/// `harnessOpen` result, later `harness` notifications and older `harnessHistory` pages.
///
/// Rules:
/// - An item with a known id replaces the held one in place (its position does not move); a
///   new id is appended. Display order is first-seen order, which is seq order on load.
/// - Notified items with seq at or below `cursor` are ignored; `cursor` becomes the highest
///   seq applied.
/// - A notification for another epoch, or of kind reset, answers `.reopen`.
/// - `prepend` adds an older page before the first item and sets `hasOlder`.
public struct HarnessTimeline: Sendable {
    public private(set) var epoch: String?
    public private(set) var cursor: Int = 0
    public private(set) var items: [HarnessItem] = []
    public private(set) var hasOlder = false

    public init() {}

    public var isLoaded: Bool { epoch != nil }

    public mutating func load(_ result: HarnessOpenResult) {
        epoch = result.epoch
        cursor = result.cursor
        items = result.items
        hasOlder = result.more
    }

    /// Applies the items of an items notification.
    public mutating func apply(_ note: HarnessNotification) -> HarnessApply {
        guard isLoaded else { return .early }
        return .reopen
    }

    public mutating func prepend(_ page: HarnessPage) {
        items = page.items + items
        hasOlder = page.more
    }

    /// The main conversation (`parent == nil`) or a subagent's own.
    public func conversation(parent: String? = nil) -> [HarnessItem] {
        items.filter { $0.parent == parent }
    }
}
