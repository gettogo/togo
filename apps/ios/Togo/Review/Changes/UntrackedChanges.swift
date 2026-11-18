import Foundation
import TogoKit

/// Groups new folders without scanning the whole status for each untracked path.
enum UntrackedChanges {
    private final class Folder {
        var children: [String: Folder] = [:]
        var count = 0
        var tracked = false
        var group: Int?
    }

    private struct Entry {
        var path: String
        var members: [String]
        var folder: Bool
    }

    static func rows(_ files: [GitStatus.File]) -> [ChangeRow] {
        let root = Folder()
        for file in files {
            var node = root
            for component in file.path.split(separator: "/", omittingEmptySubsequences: false).dropLast() {
                let name = String(component)
                if let child = node.children[name] {
                    node = child
                } else {
                    let child = Folder()
                    node.children[name] = child
                    node = child
                }
                node.count += 1
                if file.index != "?" { node.tracked = true }
            }
        }
        var entries: [Entry] = []
        for file in files where file.index == "?" {
            let parts = file.path.split(separator: "/", omittingEmptySubsequences: false)
            var node = root
            var group: (Folder, Int)?
            for (offset, component) in parts.dropLast().enumerated() {
                guard let child = node.children[String(component)] else { break }
                node = child
                if node.count >= 2, !node.tracked {
                    group = (node, offset + 1)
                    break
                }
            }
            if let (folder, depth) = group {
                if let index = folder.group {
                    entries[index].members.append(file.path)
                } else {
                    folder.group = entries.count
                    entries.append(Entry(path: parts.prefix(depth).joined(separator: "/") + "/",
                                         members: [file.path], folder: true))
                }
            } else {
                entries.append(Entry(path: file.path, members: [file.path], folder: false))
            }
        }
        return entries.map { entry in
            ChangeRow(section: .untracked, path: entry.path, paths: entry.members,
                      change: entry.folder ? .untrackedFolder(entry.members.count) : .untracked)
        }
    }
}
