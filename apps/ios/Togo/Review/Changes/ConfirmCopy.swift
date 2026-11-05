import Foundation
import TogoKit

/// A discard the user still has to answer. It names every file it acts on, and nothing is sent
/// to the machine until the user picks the destructive button.
struct Confirmation: Equatable, Identifiable {
    var title: String
    var message: String
    var button: String
    /// Every path the discard acts on, in the order the list shows them.
    var files: [String]
    let selection: ReviewSelection

    var id: [String] { files }
}

/// What the changes screen says. Keys live under `review.changes.`.
enum ChangesCopy {
    // MARK: Rows

    static func words(_ change: ChangeRow.Change) -> String {
        switch change {
        case .modified: String(localized: "review.changes.modified", defaultValue: "Modified")
        case .added: String(localized: "review.changes.added", defaultValue: "Added")
        case .deleted: String(localized: "review.changes.deleted", defaultValue: "Deleted")
        case .typeChanged: String(localized: "review.changes.typeChanged", defaultValue: "Type changed")
        case .renamed(let from):
            String(localized: "review.changes.renamed", defaultValue: "Renamed from \(DisplayText.clean(from))")
        case .copied(let from):
            String(localized: "review.changes.copied", defaultValue: "Copied from \(DisplayText.clean(from))")
        case .untracked: untracked
        case .untrackedFolder(let count):
            String(localized: "review.changes.untrackedFolder", defaultValue: "Untracked, \(count) files")
        case .conflict: String(localized: "review.changes.conflict", defaultValue: "Conflict")
        }
    }

    static let untracked = String(localized: "review.changes.untracked", defaultValue: "Untracked")

    static func sectionTitle(_ kind: ChangeSection.Kind) -> String {
        switch kind {
        case .staged: String(localized: "review.changes.section.staged", defaultValue: "Staged")
        case .changed: String(localized: "review.changes.section.changed", defaultValue: "Not Staged")
        case .untracked: String(localized: "review.changes.section.untracked", defaultValue: "Untracked")
        case .conflicted: String(localized: "review.changes.section.conflicted", defaultValue: "Conflicts")
        }
    }

    static func rowLabel(name: String, folder: String, words: String, section: ChangeSection.Kind) -> String {
        var parts = [name]
        if !folder.isEmpty {
            parts.append(String(localized: "review.changes.inFolder", defaultValue: "in \(folder)"))
        }
        parts.append(words)
        switch section {
        case .staged: parts.append(String(localized: "review.changes.staged", defaultValue: "staged"))
        case .changed: parts.append(String(localized: "review.changes.notStaged", defaultValue: "not staged"))
        case .untracked, .conflicted: break
        }
        return parts.joined(separator: ", ")
    }

    // MARK: Header

    static func summary(files: Int, staged: Int) -> String {
        let changed = files == 1
            ? String(localized: "review.changes.summary.one", defaultValue: "1 changed file")
            : String(localized: "review.changes.summary.many", defaultValue: "\(files) changed files")
        guard staged > 0 else { return changed }
        return String(localized: "review.changes.summary.staged", defaultValue: "\(changed), \(staged) staged")
    }

    static func branchLine(ahead: Int, behind: Int, upstream: String?) -> String? {
        guard let upstream else {
            return ahead > 0 ? String(localized: "review.changes.aheadLocal", defaultValue: "\(ahead) ahead") : nil
        }
        switch (ahead, behind) {
        case (0, 0):
            return String(localized: "review.changes.upToDate", defaultValue: "Up to date with \(upstream)")
        case (_, 0):
            return String(localized: "review.changes.ahead", defaultValue: "\(ahead) ahead of \(upstream)")
        case (0, _):
            return String(localized: "review.changes.behind", defaultValue: "\(behind) behind \(upstream)")
        default:
            return String(localized: "review.changes.aheadBehind", defaultValue: "\(ahead) ahead, \(behind) behind \(upstream)")
        }
    }

    static let detached = String(localized: "review.changes.detached", defaultValue: "Detached HEAD")
    static let noChanges = String(localized: "review.changes.empty.title", defaultValue: "No changes")
    static let noChangesMessage = String(
        localized: "review.changes.empty.message", defaultValue: "Everything matches the last commit."
    )
    static let notRepo = String(localized: "review.changes.notRepo.title", defaultValue: "Not a git repository")
    static let notRepoMessage = String(
        localized: "review.changes.notRepo.message", defaultValue: "This session's folder isn't inside a git repository."
    )
    static let loadFailed = String(localized: "review.changes.failed.title", defaultValue: "Couldn't load changes")
    static let loading = String(localized: "review.changes.loading", defaultValue: "Loading changes")
    static let truncated = String(
        localized: "review.changes.truncated",
        defaultValue: "Only some of the changes are listed. Stage and discard files one by one."
    )

    static let readOnly = String(
        localized: "review.changes.readOnly",
        defaultValue: "This session is not managed by Togo, so changes are read-only here."
    )
    static func helperTooOld(_ machine: String) -> String {
        String(
            localized: "review.changes.helperTooOld",
            defaultValue: "The helper on \(machine) can't change files yet. Update it to stage and commit here."
        )
    }

    // MARK: Actions

    static let stage = String(localized: "review.changes.stage", defaultValue: "Stage")
    static let unstage = String(localized: "review.changes.unstage", defaultValue: "Unstage")
    static let discard = String(localized: "review.changes.discard", defaultValue: "Discard")
    static let delete = String(localized: "review.changes.delete", defaultValue: "Delete")
    static let undoRename = String(localized: "review.changes.undoRename", defaultValue: "Undo Rename")
    static let stageAll = String(localized: "review.changes.stageAll", defaultValue: "Stage All")
    static let unstageAll = String(localized: "review.changes.unstageAll", defaultValue: "Unstage All")
    static func sectionActions(_ title: String) -> String {
        String(localized: "review.changes.sectionActions", defaultValue: "\(title) actions")
    }
    static let commit = String(localized: "review.changes.commit", defaultValue: "Commit")
    static let dismiss = String(localized: "review.changes.dismiss", defaultValue: "Dismiss")

    // MARK: Refusals

    /// What to say when the machine refused or half did a write.
    static func refusal(_ error: any Error, machine: String) -> String {
        if let error = error as? HelperError, let conflict = error.gitConflict {
            switch conflict {
            case .stale: return ReviewCopy.stale
            case .locked:
                return String(localized: "review.changes.locked", defaultValue: "Git is busy in this folder. Try again in a moment.")
            case .filtered:
                return String(
                    localized: "review.changes.filtered",
                    defaultValue: "This file uses a git filter such as LFS. Stage it at your desk."
                )
            case .unmerged: return String(localized: "review.changes.unmerged", defaultValue: "Resolve the conflict first.")
            case .nothingStaged:
                return String(localized: "review.changes.nothingStaged", defaultValue: "Nothing is staged to commit.")
            case .identity:
                return String(
                    localized: "review.changes.identity", defaultValue: "Set your git name and email on \(machine) first."
                )
            case .unbornAmend:
                return String(localized: "review.changes.unbornAmend", defaultValue: "There is no commit to amend yet.")
            case .caseRename:
                return String(
                    localized: "review.changes.caseRename",
                    defaultValue: "Can't undo a rename that only changes letter case from the phone. Use git on \(machine)."
                )
            case .partial:
                if case .partialWrite(let changed) = error { return partial(changed) }
                return partial([])
            case .raced:
                // The helper's sentence names the commit made.
                return error.localizedDescription
            }
        }
        return error.localizedDescription
    }

    /// Said after the list was read again, so it shows where things stand. Names at most three.
    static func partial(_ changed: [String]) -> String {
        guard !changed.isEmpty else {
            return String(
                localized: "review.changes.partialUnknown",
                defaultValue: "Only some of the changes were made. The list shows where things stand now."
            )
        }
        let shown = Array(changed.prefix(3))
        let names = changed.count > shown.count
            ? String(
                localized: "review.changes.partialMore",
                defaultValue: "\(shown.joined(separator: ", ")) and \(changed.count - shown.count) more"
            )
            : ListFormatter.localizedString(byJoining: shown)
        return String(
            localized: "review.changes.partial",
            defaultValue: "Only some changes were made: \(names). The list shows where things stand now."
        )
    }

    // MARK: Commit

    static let commitTitle = String(localized: "review.changes.commit.title", defaultValue: "Commit")
    static let cancel = String(localized: "review.changes.commit.cancel", defaultValue: "Cancel")
    static let messagePlaceholder = String(
        localized: "review.changes.commit.placeholder", defaultValue: "Summary on the first line, details below"
    )
    static let messageLabel = String(localized: "review.changes.commit.message", defaultValue: "Commit message")
    static let amend = String(localized: "review.changes.commit.amend", defaultValue: "Amend the last commit")
    static let amendButton = String(localized: "review.changes.commit.amendButton", defaultValue: "Amend")
    static let amendQuestion = String(localized: "review.changes.commit.amendQuestion", defaultValue: "Replace the last commit?")
    static func replaces(_ commit: GitCommit) -> String {
        String(localized: "review.changes.commit.replaces", defaultValue: "This replaces \(commit.short) \(commit.displaySubject).")
    }
    static let pushed = String(
        localized: "review.changes.commit.pushed",
        defaultValue: "This commit is already pushed. Amending it rewrites shared history."
    )
    static let footnote = String(
        localized: "review.changes.commit.footnote", defaultValue: "Commits from Togo skip git hooks and signing."
    )
    static func stagedCount(_ count: Int) -> String {
        count == 1
            ? String(localized: "review.changes.commit.stagedOne", defaultValue: "1 staged file")
            : String(localized: "review.changes.commit.stagedMany", defaultValue: "\(count) staged files")
    }
    static func firstLineLength(_ count: Int) -> String {
        String(localized: "review.changes.commit.firstLine", defaultValue: "\(count) characters on the first line")
    }

    // MARK: Confirmations

    /// The confirmation for discarding the files in `selection`, which are entries of `status`.
    /// `folder` is set when they are every listed file of one untracked folder. The folder
    /// itself stays, so the question only ever names the files. Every name is the whole path
    /// from the repository root, hidden characters marked, so the question names exactly the
    /// file that goes; `files` keeps the paths as they are.
    static func confirmation(_ selection: ReviewSelection, status: GitStatus, folder: String?) -> Confirmation {
        let files = selection.paths
        let entries = files.compactMap { path in status.files.first { $0.path == path } }
        let deletes = entries.filter(neverCommitted)
        let renames = entries.compactMap { file in
            renamedFrom(file).map { (old: DisplayText.clean($0), new: DisplayText.clean(file.path)) }
        }
        if let folder = folder.map(DisplayText.clean) {
            return Confirmation(
                title: String(
                    localized: "review.changes.confirm.folder", defaultValue: "Delete the \(files.count) files in \(folder)?"
                ),
                message: list(files) + "\n\n" + String(
                    localized: "review.changes.confirm.folderDetail",
                    defaultValue: "They were never committed, so they cannot be recovered."
                ),
                button: delete, files: files, selection: selection
            )
        }
        if files.count == 1, let path = files.first {
            let name = DisplayText.clean(path)
            if let rename = renames.first {
                return Confirmation(
                    title: String(localized: "review.changes.confirm.rename", defaultValue: "Undo the rename?"),
                    message: String(
                        localized: "review.changes.confirm.renameDetail",
                        defaultValue: "\(rename.old) comes back and \(rename.new) is removed."
                    ),
                    button: undoRename, files: files, selection: selection
                )
            }
            if deletes.count == 1 {
                return Confirmation(
                    title: String(localized: "review.changes.confirm.deleteOne", defaultValue: "Delete \(name)?"),
                    message: String(
                        localized: "review.changes.confirm.deleteOneDetail",
                        defaultValue: "It was never committed, so it cannot be recovered."
                    ),
                    button: delete, files: files, selection: selection
                )
            }
            return Confirmation(
                title: String(localized: "review.changes.confirm.one", defaultValue: "Discard changes to \(name)?"),
                message: String(
                    localized: "review.changes.confirm.oneDetail", defaultValue: "This puts it back as it was in the last commit."
                ),
                button: discard, files: files, selection: selection
            )
        }
        var message = list(files) + "\n\n"
        message += deletes.isEmpty
            ? String(
                localized: "review.changes.confirm.severalRestore", defaultValue: "They go back to how they were in the last commit."
            )
            : String(
                localized: "review.changes.confirm.severalDetail",
                defaultValue: "Files that were never committed are deleted and cannot be recovered. The rest go back to the last commit."
            )
        for rename in renames {
            message += " " + String(
                localized: "review.changes.confirm.severalRename",
                defaultValue: "\(rename.old) comes back and \(rename.new) is removed."
            )
        }
        return Confirmation(
            title: String(localized: "review.changes.confirm.several", defaultValue: "Discard changes to \(files.count) files?"),
            message: message, button: discard, files: files, selection: selection
        )
    }

    /// The old path of a rename staged in the index; discarding the rename brings it back.
    static func renamedFrom(_ file: GitStatus.File) -> String? {
        guard file.index == "R", let orig = file.origPath, !orig.isEmpty else { return nil }
        return orig
    }

    /// Discarding a file HEAD does not have deletes it.
    static func neverCommitted(_ file: GitStatus.File) -> Bool {
        file.index == "?" || file.index == "A" || file.index == "C"
    }

    private static func list(_ files: [String]) -> String {
        files.map(DisplayText.clean).joined(separator: "\n")
    }
}
