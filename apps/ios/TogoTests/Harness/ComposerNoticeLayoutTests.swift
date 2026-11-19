import SwiftUI
import Testing
import UIKit
@testable import Togo

@MainActor struct ComposerNoticeLayoutTests {
    @Test(arguments: [CGFloat(240), CGFloat(380)], [DynamicTypeSize.large, .accessibility3])
    func aRefusalReservesAllItsWrappedLines(width: CGFloat, type: DynamicTypeSize) {
        let text = "Not sent. The input already contains a draft that could not be safely replaced. Close the open dialog on the machine, then try again. " + String(repeating: "The pending draft was kept. ", count: 5)
        let words = UIHostingController(rootView: Text(text).font(.footnote.weight(.semibold))
            .fixedSize(horizontal: false, vertical: true).environment(\.dynamicTypeSize, type))
        let needed = words.sizeThatFits(in: .init(width: width - 32, height: 3000)).height
        let host = UIHostingController(rootView: ComposerNoticeLine(text: text, action: .retry) { _ in }
            .environment(\.dynamicTypeSize, type))
        let size = host.sizeThatFits(in: .init(width: width, height: 120))
        #expect(size.height >= needed + 10, "Meaningful refusal text must not be compressed into an ellipsis")
    }
}
