import SwiftUI
import TogoKit

/// What the bar shows besides the field, mirrored so each change is one transaction.
struct ComposerParts: Equatable {
    enum Part: Hashable { case menu, queued, notice, undo, attachNotice, pictures }

    /// The Undo a restart offers, compared by id.
    struct Undo: Equatable {
        var id: UUID
        var title: String
        var run: @MainActor () async -> Void

        static func == (a: Undo, b: Undo) -> Bool { a.id == b.id }
    }

    var menu = false
    var queued = 0
    var notice: String?
    var undo: Undo?
    var attachNotice: String?
    var pictures = false

    @MainActor init(_ model: CommandLineModel) {
        menu = model.menu != .none
        queued = model.queuedCount
        notice = model.notice
        undo = model.store.undo.map { Undo(id: $0.id, title: $0.title, run: $0.run) }
        attachNotice = model.attachments.notice
        pictures = !model.attachments.isEmpty
    }

    init() {}

    /// The parts present, for telling what leaves.
    var parts: Set<Part> {
        var out: Set<Part> = []
        if menu { out.insert(.menu) }
        if queued > 0 { out.insert(.queued) }
        if notice != nil { out.insert(.notice) }
        if undo != nil { out.insert(.undo) }
        if attachNotice != nil { out.insert(.attachNotice) }
        if pictures { out.insert(.pictures) }
        return out
    }
}

/// A screen id the composer opens in a sheet.
struct ScreenRequest: Identifiable {
    var id: String
}

/// A notice under the field: what did not work, and the one thing to do about it ("Open
/// terminal" after a stop that did not land, "Try again" after a prompt that did not go).
struct ComposerNoticeLine: View {
    let text: String
    let action: NoticeAction?
    let perform: (NoticeAction) -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 10) { words; button }
            VStack(alignment: .leading, spacing: 4) { words; button }
        }
        .padding(.horizontal, 16)
        .padding(.top, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var words: some View {
        Text(text)
            .font(.footnote.weight(.semibold))
            // A hint about the words in the field is not a failure.
            .foregroundStyle(action == .sendAsText ? Color.togoMute : PrimitiveTint.fail)
            .accessibilityAddTraits(.updatesFrequently)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var button: some View {
        if let action {
            Button(label(action)) { perform(action) }
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .fixedSize()
                .frame(minHeight: 44)
        }
    }
}

private func label(_ action: NoticeAction) -> String {
    switch action {
    case .openTerminal: ComposerCopy.openTerminal
    case .retry: ComposerCopy.tryAgain
    case .sendAsText: ComposerCopy.sendAsText
    }
}

/// "Back to Opus": the Undo a restart offers, for a few seconds after it.
struct UndoLine: View {
    let title: String
    let undo: () -> Void

    var body: some View {
        Button(action: undo) {
            Label(title, systemImage: "arrow.uturn.backward")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 16)
        .padding(.top, 2)
    }
}

/// The `/` menu opening upward from the composer's edge: its content sits in its final place
/// and an edge anchored at the bottom uncovers it, on the transaction that grows the glass.
struct UnfoldingUp: Transition {
    func body(content: Content, phase: TransitionPhase) -> some View {
        content.mask(alignment: .bottom) {
            Rectangle().scaleEffect(x: 1, y: phase == .willAppear ? 0 : 1, anchor: .bottom)
        }
    }
}

/// The small capsules under the field: the state chip, commands and @.
struct ComposerChipStyle: ButtonStyle {
    @Environment(\.displayScale) private var displayScale
    var prominent = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold))
            // trunc-ok: chips are icons, or the state chip, which overrides this and wraps
            .foregroundStyle(prominent ? Color.togoInk : Color.togoSnow)
            .lineLimit(1)
            .padding(.horizontal, 12)
            .frame(minWidth: 34, minHeight: 32)
            .background(prominent ? Color.togoSnow : Color.togoSnow.opacity(0.06), in: .capsule)
            .overlay {
                if !prominent { Capsule().strokeBorder(Color.togoLine, lineWidth: 1 / displayScale) }
            }
            .frame(minHeight: 44)
            .contentShape(.capsule)
            .opacity(configuration.isPressed ? 0.8 : 1)
    }
}
