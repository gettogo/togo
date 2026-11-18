import Foundation

extension CommandLineModel {
    /// A replaced or hidden notice cancels its task. Its old timer must not
    /// remove the notice that replaced it, or a notice kept for the next view.
    func expireNotice(_ expected: String, after delay: Duration? = nil) async {
        do {
            try await Task.sleep(for: delay ?? .seconds(noticeAction == nil ? 5 : 8))
        } catch {
            return
        }
        guard !Task.isCancelled, notice == expected else { return }
        clearNotice()
    }
}
