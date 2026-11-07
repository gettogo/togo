import Foundation

/// The recent folders the folder step lists: a handful the user returns to, never temporary
/// folders, and each one short enough to read at a glance.
///
/// The helper already leaves temporary and missing folders out and keeps five; this applies
/// the same rule to what an older helper sends.
enum RecentFolders {
    /// How many recent folders show.
    static let limit = 5

    /// `dirs` without temporary folders or repeats, at most `limit`, in the order given.
    static func shown(_ dirs: [String]) -> [String] {
        var seen = Set<String>()
        return Array(dirs.filter { !isTemporary($0) && seen.insert($0).inserted }.prefix(limit))
    }

    /// A throwaway folder: under /tmp, /private/tmp or /var/folders (macOS's per-user temporary
    /// folders), or anywhere inside a folder named "scratchpad".
    static func isTemporary(_ path: String) -> Bool {
        let parts = path.split(separator: "/").map(String.init)
        if parts.contains("scratchpad") { return true }
        let roots = ["/tmp", "/private/tmp", "/var/folders", "/private/var/folders"]
        return roots.contains { path == $0 || path.hasPrefix($0 + "/") }
    }

    /// The path as a row shows it: `~` for the home folder, the folders in between collapsed to
    /// "…" when there are many, and the folder itself apart so it can stand out.
    /// "/Users/me/Dev/togo/.worktrees/fix" → ("~/…/.worktrees/", "fix").
    static func display(_ path: String) -> (parent: String, name: String) {
        let shown = PathText.abbreviate(path)
        guard shown != "/", shown != "~" else { return ("", shown) }
        let absolute = shown.hasPrefix("/")
        var parts = shown.split(separator: "/", omittingEmptySubsequences: true).map(String.init)
        guard let name = parts.popLast() else { return ("", shown) }
        // Keep the root folder and the one holding the folder; collapse what lies between.
        if parts.count > 2 {
            parts = [parts[0], "…", parts[parts.count - 1]]
        }
        let parent = (absolute ? "/" : "") + parts.map { $0 + "/" }.joined()
        return (parent, name)
    }
}
