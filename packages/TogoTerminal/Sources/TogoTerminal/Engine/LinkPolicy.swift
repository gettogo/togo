import Foundation

/// Decides which links from terminal output may be opened.
///
/// Remote output is untrusted: an OSC 8 link can carry any scheme, and `javascript:`,
/// `file:` or an app scheme (including Togo's own) would turn printed text into an action
/// on this device. Only web links leave the terminal.
public enum LinkPolicy {
    public static func allows(_ url: URL) -> Bool {
        guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else {
            return false
        }
        return url.host?.isEmpty == false
    }
}
