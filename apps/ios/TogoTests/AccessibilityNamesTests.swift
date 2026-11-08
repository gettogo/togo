import SwiftUI
import Testing
@testable import Togo

struct AccessibilityNamesTests {
    @Test func sessionAnswersToItsShortNames() {
        #expect(VoiceControlNames.session(name: "my-api", bib: "04", agent: "Claude Code")
            == ["my-api", "04 my-api", "04", "Claude Code"])
        #expect(VoiceControlNames.session(name: "Codex", bib: nil, agent: "codex") == ["Codex"])
    }

    @Test func blanksAndRepeatsAreLeftOut() {
        #expect(VoiceControlNames.make([" ", nil, "Sonnet", "sonnet", "Model"]) == ["Sonnet", "Model"])
    }
}

struct ContrastTests {
    @Test func increaseContrastThickensHairlinesAndFills() {
        #expect(Contrast.hairline(3, .standard) == 1.0 / 3)
        #expect(Contrast.hairline(3, .increased) == 1)
        #expect(Contrast.fill(0.06, .standard) == 0.06)
        #expect(Contrast.fill(0.06, .increased) > 0.06)
        #expect(Contrast.fill(0.6, .increased) == 1)
        #expect(Contrast.outlinesSurfaces(.increased) && !Contrast.outlinesSurfaces(.standard))
    }
}
