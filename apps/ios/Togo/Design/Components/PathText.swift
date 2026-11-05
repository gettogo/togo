import SwiftUI

/// A path on a machine, in monospace, shortened in the middle so both the root and the
/// folder name stay readable.
struct PathText: View {
    let path: String
    var style: Font.TextStyle = .footnote

    init(_ path: String, style: Font.TextStyle = .footnote) {
        self.path = path
        self.style = style
    }

    var body: some View {
        Text(verbatim: Self.abbreviate(path))
            .font(.system(style, design: .monospaced))
            .foregroundStyle(Color.togoMute)
            .lineLimit(1)
            .truncationMode(.middle)
    }

    /// Replaces the home folder of the machine the path came from with `~`. The phone does not
    /// know the remote user's home, so it recognises the usual Unix and macOS layouts.
    nonisolated static func abbreviate(_ path: String) -> String {
        if path == "/root" || path.hasPrefix("/root/") {
            return "~" + path.dropFirst("/root".count)
        }
        for base in ["/Users/", "/home/"] where path.hasPrefix(base) {
            let rest = path.dropFirst(base.count)
            guard let user = rest.split(separator: "/", maxSplits: 1).first, !user.isEmpty else { continue }
            return "~" + rest.dropFirst(user.count)
        }
        return path
    }
}
