import Foundation
import TogoKit

/// The files a write acts on, with what the user saw of each. The helper refuses the write as
/// stale when a file has changed since, so a selection is always built from the status the
/// screen displayed and never from paths alone.
struct ReviewSelection: Equatable, Sendable {
    enum Failure: Error, Equatable, LocalizedError {
        /// The list shown leaves files out, so "all changes" would include files never seen.
        case truncated
        case notShown(String)
        case invalidPath(String)
        case empty

        var errorDescription: String? {
            switch self {
            case .truncated: ReviewCopy.truncatedAll
            case .notShown: ReviewCopy.stale
            case .invalidPath(let path): ReviewCopy.invalidPath(path)
            case .empty: ReviewCopy.nothingSelected
            }
        }
    }

    private(set) var paths: [String]
    private(set) var seen: [String: GitSeen]

    /// Exactly `paths`, each of which must be an entry of the displayed `status`.
    init(_ status: GitStatus, paths: [String]) throws {
        guard !paths.isEmpty else { throw Failure.empty }
        var seen: [String: GitSeen] = [:]
        for path in paths {
            guard GitPathsParams.isValidPath(path) else { throw Failure.invalidPath(path) }
            guard let file = status.files.first(where: { $0.path == path }) else { throw Failure.notShown(path) }
            seen[path] = GitSeen(file)
            // Discarding a staged rename brings its old path back, so that path is part of
            // what the user saw: same letters, no file there.
            if file.index == "R", let orig = file.origPath, !orig.isEmpty {
                seen[orig] = GitSeen(status: file.letters, fingerprint: nil)
            }
        }
        self.paths = paths
        self.seen = seen
    }

    /// Every displayed file `include` admits. Throws `.truncated` when the status leaves files out.
    static func all(in status: GitStatus, where include: (GitStatus.File) -> Bool = { _ in true }) throws -> ReviewSelection {
        guard !status.isTruncated else { throw Failure.truncated }
        return try ReviewSelection(status, paths: status.files.filter(include).map(\.path))
    }
}
