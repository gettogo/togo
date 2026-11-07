import SwiftUI
import UIKit

/// The microphone beside Send. Hold to talk and let go to add the words; a quick tap keeps it
/// listening until the next tap. VoiceOver, Switch Control and Voice Control cannot hold, so
/// their activation starts and stops it the same way a tap does.
struct DictationButton: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .body) private var diameter: CGFloat = 32
    /// True while a finger is down. Gesture state resets on its own when the system cancels
    /// the touch (a call, Control Center, a sheet), which lifts the hold like a release.
    @GestureState private var touching = false
    let model: DictationModel

    var body: some View {
        let on = model.isActive
        Image(systemName: on ? "mic.fill" : "mic")
            .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
            .font(.body.weight(on ? .semibold : .regular))
            .foregroundStyle(on ? Color.togoInk : Color.togoMute)
            .frame(width: diameter, height: diameter)
            .background(Circle().fill(on ? Color.togoSnow : Color.clear))
            .frame(minWidth: 44, minHeight: 44)
            // Without this the minimum frame stretches and takes width from the text field.
            .fixedSize()
            .contentShape(Rectangle())
            .gesture(DragGesture(minimumDistance: 0).updating($touching) { _, touching, _ in touching = true })
            .onChange(of: touching) { old, new in
                switch HoldEdge(from: old, to: new) {
                case .press?: Task { await model.press() }
                case .release?: Task { await model.release() }
                case nil: break
                }
            }
            .onChange(of: reduceMotion, initial: true) { model.showsLevel = !reduceMotion }
            .accessibilityElement()
            .accessibilityLabel(on ? DictationCopy.stop : DictationCopy.dictate)
            .accessibilityHint(on ? "" : DictationCopy.hint)
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { Task { await model.toggle() } }
            .motion(.respond, value: on)
            .haptic(.listen, trigger: model.isListening) { $0 }
    }
}

/// A change in whether a finger is on the microphone.
enum HoldEdge: Equatable {
    case press, release

    init?(from old: Bool, to new: Bool) {
        switch (old, new) {
        case (false, true): self = .press
        case (true, false): self = .release
        default: return nil
        }
    }
}

/// The line above the field while dictating: what has been heard so far, a download's
/// progress, or why dictation could not start and what to do about it.
struct DictationLine: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.openURL) private var openURL
    let model: DictationModel
    /// The phase as the compose bar shows it, so a line leaving keeps its words while it fades.
    var phase: DictationModel.Phase?

    var body: some View {
        switch phase ?? model.phase {
        case .idle, .finishing, .preparing(nil):
            EmptyView()
        case .preparing(let fraction?):
            downloading(fraction)
        case .listening(let partial, let level):
            listening(partial, level: level)
        case .failed(let message):
            failed(message)
        }
    }

    private func listening(_ partial: String, level: Float) -> some View {
        HStack(alignment: .center, spacing: 10) {
            if reduceMotion {
                // No moving meter: the word says it instead, and stays beside what is heard.
                Text(DictationCopy.listening)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.togoMute)
                if !partial.isEmpty { heard(partial) }
            } else {
                LevelMeter(level: level)
                if partial.isEmpty {
                    Text(DictationCopy.listening)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                } else {
                    heard(partial)
                }
            }
        }
        // "Listening" gives way to the first words heard in place.
        .motion(.respond, value: partial.isEmpty)
        .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(partial.isEmpty ? DictationCopy.listening : "\(DictationCopy.listening), \(partial)")
    }

    /// The newest words matter most, so a long partial loses its start rather than its end.
    private func heard(_ partial: String) -> some View {
        Text(partial)
            .font(.subheadline)
            .foregroundStyle(Color.togoSnow)
            // trunc-ok: a live partial while listening; the whole text lands in the field when dictation ends
            .lineLimit(dynamicTypeSize.isAccessibilitySize ? 4 : 2)
            .truncationMode(.head)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func downloading(_ fraction: Double) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(DictationCopy.downloading(language: DictationCopy.language(for: model.locale)))
                .font(.subheadline)
                .foregroundStyle(Color.togoSnow)
            ProgressView(value: fraction)
                .tint(Color.togoSnow)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder private func failed(_ message: String) -> some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 4) {
                failure(message)
                HStack(spacing: 12) {
                    settingsButton
                    downloadButton
                    Spacer(minLength: 0)
                    dismissButton
                }
            }
            .padding(.leading, 16)
            .padding(.trailing, 4)
            .padding(.vertical, 4)
        } else {
            HStack(alignment: .center, spacing: 12) {
                failure(message)
                settingsButton
                downloadButton
                dismissButton
            }
            .padding(.leading, 16)
            .padding(.trailing, 4)
            .padding(.vertical, 4)
        }
    }

    private func failure(_ message: String) -> some View {
        Text(message)
            .font(.subheadline)
            .foregroundStyle(Color.togoSnow)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private var settingsButton: some View {
        if model.opensSettings, let url = URL(string: UIApplication.openSettingsURLString) {
            Button(DictationCopy.openSettings) { openURL(url) }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
                .fixedSize()
        }
    }

    @ViewBuilder private var downloadButton: some View {
        if model.offersDownload {
            Button(DictationCopy.download) { Task { await model.download() } }
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
                .fixedSize()
        }
    }

    private var dismissButton: some View {
        Button(action: model.dismiss) {
            Image(systemName: "xmark")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .frame(minWidth: 44, minHeight: 44)
                .fixedSize()
                .contentShape(Rectangle())
        }
        .accessibilityLabel(DictationCopy.dismiss)
    }
}

/// Four bars that rise with the microphone's level.
private struct LevelMeter: View {
    @ScaledMetric(relativeTo: .subheadline) private var height: CGFloat = 16
    let level: Float

    private static let weights: [CGFloat] = [0.55, 1, 0.75, 0.4]

    var body: some View {
        HStack(alignment: .center, spacing: 2) {
            ForEach(Self.weights.indices, id: \.self) { index in
                Capsule()
                    .fill(Color.togoSnow)
                    .frame(width: 3, height: max(3, height * Self.weights[index] * CGFloat(level)))
            }
        }
        .frame(height: height)
        .motion(.listen, value: level)
        .accessibilityHidden(true)
    }
}
