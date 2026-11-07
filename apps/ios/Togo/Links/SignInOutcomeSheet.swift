import SwiftUI
import TogoKit

/// What a web sign-in opened from a link or a request card came to, once the system browser
/// closed: "Signed in to Anthropic", or the provider's code to paste back, with Open again.
struct SignInOutcomeSheet: View {
    let runner: SignInRunner
    /// Nil when the sign-in has no session to type a code into.
    let paste: PasteCodeModel?
    let handle: SheetHandle

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    switch runner.phase {
                    case .signedIn, .delivered:
                        signedIn
                    case .failed(let message):
                        Text(message)
                            .font(.body)
                            .foregroundStyle(Color.togoSnow)
                            .fixedSize(horizontal: false, vertical: true)
                        reopen
                    default:
                        if let note = runner.note {
                            Text(note)
                                .font(.footnote)
                                .foregroundStyle(Color.togoMute)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        if let paste {
                            PasteCodeField(model: paste)
                        } else {
                            Text(PasteCodeCopy.noSession)
                                .font(.footnote)
                                .foregroundStyle(Color.togoMute)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        reopen
                    }
                }
                .padding(20)
            }
            .background(Color.togoInk)
            .navigationTitle(SignInPageCopy.title(runner.page.provider))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(LinkCopy.done, systemImage: "checkmark", role: .confirm) {
                        runner.finish()
                        handle.close()
                    }
                }
            }
        }
        .togoControls()
        .haptic(.done, trigger: runner.phase, when: { $0 == .signedIn })
        // Reopened from here, the browser covers this sheet; only closing it ends the sign-in.
        .onDisappear { if runner.phase == .signedIn || runner.phase == .cancelled { runner.finish() } }
    }

    private var signedIn: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(Color.togoSnow)
                .accessibilityHidden(true)
            Text(SignInPageCopy.signedIn(runner.page.provider))
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }

    private var reopen: some View {
        Button(SignInPageCopy.openAgain) { runner.reopen() }
            .buttonStyle(.secondary)
    }
}
