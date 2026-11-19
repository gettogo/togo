import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

let geometryTextSizes: [DynamicTypeSize] = [.large, .accessibility3, .accessibility5]

private enum PrimitiveGeometrySample: String, CaseIterable, Sendable {
    case picker, settings, checklist, liveList, detail, confirm, status

    @MainActor var fixture: (screen: HarnessScreen, first: String, second: String?) {
        switch self {
        case .picker: (HarnessSamples.model, "Most capable", "Fast and capable")
        case .settings: (HarnessSamples.settings, "GPT-5.1 Codex", "Reasoning effort")
        case .checklist: (HarnessSamples.plan, "Read the failing date tests", "Parse timestamps in UTC")
        case .liveList: (HarnessSamples.jobs, "npm run dev", "go test ./...")
        case .detail:
            (HarnessScreen(id: "workspace", primitive: .detail, title: "Workspace", fetchedAt: HarnessSamples.at,
                           detail: .init(body: .init(kind: "kv", kv: [
                               .init(label: "Workspace folder", value: "Calendar migration")
                           ]), sections: [
                               .init(title: "Verification", body: .init(kind: "kv", kv: [
                                   .init(label: "State", value: "Date checks pending")
                               ]))
                           ])), "Workspace folder", "Verification")
        case .confirm: (HarnessSamples.question, "Two things before I migrate staging.", "Which environment should the backfill run against?")
        case .status: (HarnessSamples.usage, "opus", "Ask")
        }
    }
}

/// Synthetic screens exercise each renderer and modeled layout in the host. These tests
/// don't attest to real adapter output or replace the per-adapter app coverage cells.
@MainActor
@Suite(.serialized)
struct PrimitiveWholeScreenGeometryTests {
    private struct Case: CustomTestStringConvertible, Sendable {
        let sample: PrimitiveGeometrySample
        let layout: ScreenSize
        let size: DynamicTypeSize

        var testDescription: String { "\(sample.rawValue)/\(layout.rawValue)/\(size)" }
    }

    nonisolated private static let cases = PrimitiveGeometrySample.allCases.flatMap { sample in
        ScreenSize.allCases.flatMap { layout in
            geometryTextSizes.map {
                Case(sample: sample, layout: layout, size: $0)
            }
        }
    }

    @Test(arguments: cases)
    private func initialViewportKeepsVisibleLabelsInsideTheScreen(_ test: Case) throws {
        let (screen, first, second) = test.sample.fixture
        let layout = test.layout
        let inset = layout.safeArea
        let content = NavigationStack {
            PrimitiveScreenView(screen: screen, context: .inert)
                .navigationBarTitleDisplayMode(.inline)
        }
        .padding(EdgeInsets(top: inset.top, leading: inset.left, bottom: inset.bottom, trailing: inset.right))
        .frame(width: layout.size.width, height: layout.size.height)
        .environment(\.dynamicTypeSize, test.size)
        .environment(\.horizontalSizeClass, layout.sizeClass)
        .environment(\.fold, layout.inSafeArea(layout.fold))
        .environment(\.barIsVerticalOverride, layout == .duoFolded ? true : nil)
        .environment(\.loopsPaused, true)

        let host = try GeometryHost(content, width: layout.size.width, height: layout.size.height)
        defer { host.close() }
        // Accessibility frames are in screen coordinates, unlike view.bounds.
        let viewport = UIAccessibility.convertToScreenCoordinates(host.view.bounds.inset(by: inset), in: host.view)
        let elements = host.elements()
        func finite(_ frame: CGRect) -> Bool {
            [frame.minX, frame.minY, frame.width, frame.height].allSatisfy(\.isFinite) && !frame.isEmpty
        }
        func visible(_ fragment: String) -> [CGRect] {
            elements.filter { $0.label.localizedCaseInsensitiveContains(fragment) }
                .map(\.frame)
                .filter { finite($0) && $0.maxY > viewport.minY && $0.minY < viewport.maxY }
        }

        let anchors = visible(first)
        try #require(!anchors.isEmpty, "Missing visible content anchor: \(first)")
        // Scroll content may extend above/below the viewport. A visible row must still
        // fit horizontally, and independent visible anchors must not cover each other.
        let peers = second.map(visible) ?? []
        for frame in anchors + peers {
            #expect(frame.minX >= viewport.minX - 1 && frame.maxX <= viewport.maxX + 1,
                    "Content crosses the safe-area width: \(frame), viewport \(viewport)")
        }
        for anchor in anchors {
            for peer in peers {
                let overlap = anchor.intersection(peer)
                #expect(overlap.isNull || overlap.width <= 1 || overlap.height <= 1,
                        "Visible content anchors overlap: \(anchor), \(peer)")
            }
        }
    }
}
