import Foundation
import TogoKit

/// What the review screens ask of a machine. Kept apart from `AppServices` so the screens, the
/// terminal and the control center do not share one growing protocol.
///
/// Writes carry what the user saw. Callers build `paths` and `seen` with `ReviewSelection` from
/// the status on screen, and `head` from the history on screen. There is no "all" write: a
/// selection of everything is refused while `GitStatus.isTruncated`, since it would act on files
/// the user never saw. The helper refuses a changed file as `HelperError.conflict("stale")`.
@MainActor protocol ReviewServices: AnyObject {
    func status(_ s: ReviewSubject) async throws -> GitStatus
    /// Never called with `.untracked`: the diff model reads the file and builds an added-file diff.
    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff
    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult
    /// An empty `path` is the session cwd. Without a `ref` a session path joins onto `s.cwd`.
    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult
    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit]
    func branches(_ s: ReviewSubject) async throws -> GitBranches
    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus
    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus
    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus
    /// `head` is the hash of the latest commit the user saw; an amend replaces exactly that commit.
    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult
    func ports(_ s: ReviewSubject) async throws -> [ListeningPort]
    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo
    func stopForward(localPort: Int) async
    /// Forwards that ended on their own, or whose server stopped answering, with what to tell the
    /// user. Each call returns its own stream; it ends when its reader is cancelled.
    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)>
}

extension ReviewServices {
    func read(_ s: ReviewSubject, path: String, ref: String?) async throws -> FsReadResult {
        try await read(s, path: path, ref: ref, base: .session)
    }

    func listDir(_ s: ReviewSubject, path: String, ref: String?) async throws -> ListDirResult {
        try await listDir(s, path: path, ref: ref, base: .session)
    }
}

struct ReviewUnavailable: Error, LocalizedError {
    var errorDescription: String? { ReviewCopy.unavailable }
}

/// Stands in until a store is given real services.
@MainActor final class NoReviewServices: ReviewServices {
    func status(_ s: ReviewSubject) async throws -> GitStatus { throw ReviewUnavailable() }
    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff { throw ReviewUnavailable() }
    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        throw ReviewUnavailable()
    }
    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        throw ReviewUnavailable()
    }
    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] { throw ReviewUnavailable() }
    func branches(_ s: ReviewSubject) async throws -> GitBranches { throw ReviewUnavailable() }
    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus { throw ReviewUnavailable() }
    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus { throw ReviewUnavailable() }
    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus { throw ReviewUnavailable() }
    /// `head` is the hash of the latest commit the user saw; an amend replaces exactly that commit.
    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult { throw ReviewUnavailable() }
    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] { throw ReviewUnavailable() }
    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo { throw ReviewUnavailable() }
    func stopForward(localPort: Int) async {}
    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)> { AsyncStream { $0.finish() } }
}
