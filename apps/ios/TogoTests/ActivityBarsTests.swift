import Testing
@testable import Togo

struct ActivityBarsTests {
    @Test func heightHasAFloorOfTwoPoints() {
        #expect(ActivityBars.height(for: 0) == 2)
        #expect(ActivityBars.height(for: 0.1) == 2)
        #expect(ActivityBars.height(for: .nan) == 2)
    }

    @Test func heightScalesToTwelvePoints() {
        #expect(ActivityBars.height(for: 0.5) == 6)
        #expect(ActivityBars.height(for: 1) == 12)
        #expect(ActivityBars.height(for: 3) == 12)
        #expect(ActivityBars.height(for: -1) == 2)
    }

    @Test func showsNewestSixteen() {
        let samples = (0..<20).map { Double($0) / 20 }
        #expect(ActivityBars.visible(samples) == Array(samples.suffix(16)))
        #expect(ActivityBars.visible([]).isEmpty)
    }
}
