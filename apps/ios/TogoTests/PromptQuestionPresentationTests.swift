import Testing
@testable import Togo

struct PromptQuestionPresentationTests {
    enum Layout: CaseIterable, Sendable { case full, compact, accessibility, both }

    @Test(arguments: Layout.allCases, [nil, "", "pwd"] as [String?])
    func everyLimitedQuestionHasOneFullQuestionRoute(layout: Layout, command: String?) {
        let compact = layout == .compact || layout == .both
        let accessibility = layout == .accessibility || layout == .both
        let presentation = PromptQuestionPresentation(compact: compact, accessibilitySize: accessibility, command: command)
        let routes = [presentation.commandDisclosure, presentation.questionDisclosure].filter { $0 }.count
        #expect(routes == (presentation.lineLimit == nil ? 0 : 1))
        if command?.isEmpty == false { #expect(!presentation.questionDisclosure) }
        else { #expect(!presentation.commandDisclosure) }
    }
}
