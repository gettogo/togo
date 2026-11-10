import SwiftUI

/// The rows the terminal's input status shows, as it follows its model. A change of stage,
/// such as a row arriving, leaving or giving way to another, moves the status in one `resize`
/// transaction while the rows reveal and conceal; a change within a stage, such as the words
/// heard so far or the meter's level, lands at once.
struct ComposeShape: Equatable {
    var image: ImagePasteFlow.Phase = .idle
    var dictation: DictationModel.Phase?

    struct Stage: Equatable {
        var image = 0
        var dictation = 0
    }

    var stage: Stage {
        Stage(image: Self.stage(of: image), dictation: Self.stage(of: dictation))
    }

    /// Which image row shows: 0 for none, then one number per row, so two different messages
    /// swap rather than change in place.
    static func stage(of phase: ImagePasteFlow.Phase) -> Int {
        switch phase {
        case .idle, .annotating: 0
        case .uploading: 1
        case .failed: 2
        case .loadFailed: 3
        case .unreadable: 4
        case .tooLarge: 5
        case .unsupported: 6
        case .noHelper: 7
        }
    }

    /// Which dictation line shows, without the words and level that change many times a second.
    static func stage(of phase: DictationModel.Phase?) -> Int {
        switch phase {
        case nil, .idle?, .finishing?, .preparing(nil)?: 0
        case .preparing?: 1
        case .listening?: 2
        case .failed?: 3
        }
    }
}

extension ComposeShape {
    /// The shape on screen moves to `next`: on `resize` when its stage changes, at once otherwise.
    @MainActor static func follow(_ shown: Binding<ComposeShape>, to next: ComposeShape, reduce: Bool) {
        guard shown.wrappedValue != next else { return }
        guard shown.wrappedValue.stage != next.stage else {
            shown.wrappedValue = next
            return
        }
        // motion: preventing a jump — the status opens or closes its room with its rows, so nothing above it jumps.
        withAnimation(Motion.animation(.resize, reduce: reduce)) { shown.wrappedValue = next }
    }
}
