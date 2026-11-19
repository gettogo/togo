import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor @Suite(.serialized) struct QuestionRowLayoutTests {
    @Test(arguments: [CGFloat(320), CGFloat(700)], [DynamicTypeSize.large, .accessibility3])
    func longChoicesReserveTheirWrappedHeight(width: CGFloat, type: DynamicTypeSize) throws {
        let choices = (0..<4).map { number in
            HarnessChoice(id: "choice-\(number)", label: "Choice \(number): " + String(repeating: "Review the isolated workspace and its pending checks before proceeding. ", count: 3))
        }
        let questions: [HarnessQuestion] = [
            .init(id: "scope", prompt: "Question alpha: choose the next action", options: choices, multi: true, allowText: false),
            .init(id: "timing", prompt: "Question beta: choose when to continue", options: [.init(id: "later", label: "Continue after the remaining checks finish")], multi: false, allowText: false)
        ]
        let body = HarnessItem.Question(questions: questions)
        let item = HarnessItem(id: "questions", seq: 1, at: HarnessSamples.at, body: .question(body))
        let root = VStack(alignment: .leading) {
            QuestionRow(item: item, question: body) { _ in true }
            Spacer(minLength: 0)
        }
        .environment(\.dynamicTypeSize, type)
        .environment(\.loopsPaused, true)
        let host = try LayoutTestHost(root, width: width, height: 420)
        defer { host.close() }
        let elements = host.elements()
        var previous = try #require(elements.first { $0.label == questions[0].prompt }).frame
        for index in 0..<4 {
            let row = try #require(elements.first { $0.label.hasPrefix("Choice \(index):") }).frame
            #expect(row.minY >= previous.maxY - 1, "Wrapped choices must not overlap the heading or preceding choice")
            #expect(row.height >= 44)
            let label = UIHostingController(rootView: Text(verbatim: choices[index].label)
                .font(.body).environment(\.dynamicTypeSize, type))
            let needed = label.sizeThatFits(in: .init(width: row.width, height: 2000)).height
            #expect(row.height >= needed - 1, "The row must reserve at least the height of its text")
            previous = row
        }
        let next = try #require(elements.first { $0.label == questions[1].prompt }).frame
        #expect(next.minY >= previous.maxY - 1, "The following question must start after the complete last option")
    }
}
