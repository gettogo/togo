import Foundation
import TogoKit

extension CommandLineModel {
    /// What the menu above the field lists.
    enum Menu: Equatable {
        case none
        case commands([HarnessCompletion])
        case files([HarnessCompletion])

        var items: [HarnessCompletion] {
            switch self {
            case .none: []
            case .commands(let items), .files(let items): items
            }
        }
    }

    /// An incomplete workspace snapshot must not look like a successful search
    /// with no matches. Only this fixed category changes the composer's notice;
    /// original paths and error text never reach it.
    func showFileCompletionFailure(_ error: Error, for trigger: CommandLineLogic.Trigger) -> Bool {
        guard case .file = trigger, let helper = error as? HelperError,
              case .rpc(let object) = helper, object.code == RPCErrorCode.refused,
              object.reason == "fallback", case .object(let fields)? = object.data,
              fields["detail"] == .string("roots") else { return false }
        notice = ComposerCopy.filesUnavailable
        noticeAction = .openTerminal
        return true
    }

    func clearFileCompletionFailure() {
        guard notice == ComposerCopy.filesUnavailable, noticeAction == .openTerminal else { return }
        clearNotice()
    }
}
