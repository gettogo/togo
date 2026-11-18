import Foundation

/// Keeps the full-question route available when the compact bar limits its heading.
struct PromptQuestionPresentation {
    var lineLimit: Int?
    var commandDisclosure: Bool
    var questionDisclosure: Bool

    init(compact: Bool, accessibilitySize: Bool, command: String?) {
        lineLimit = compact ? 2 : accessibilitySize ? 3 : nil
        let hasCommand = !(command?.isEmpty ?? true)
        commandDisclosure = lineLimit != nil && hasCommand
        questionDisclosure = lineLimit != nil && !hasCommand
    }
}
