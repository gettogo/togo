import SwiftUI
import TogoKit
import UIKit

/// The servers listening on a session's machine. Each one that the machine can reach over
/// loopback opens in a preview here, through an SSH forward.
struct PortsView: View {
    @Bindable var model: PortsModel
    @Environment(\.scenePhase) private var scenePhase

    init(model: PortsModel) {
        self.model = model
    }

    var body: some View {
        content
            .phaseSwap(model.subject.canPorts ? CaseName.of(model.phase) : "unavailable")
            .background(Color.togoInk)
            .onAppear { model.appear() }
            .onDisappear { model.disappear() }
            .onChange(of: scenePhase) { _, phase in
                switch phase {
                case .active: model.setForeground(true)
                case .background: model.setForeground(false)
                default: break
                }
            }
            // Dismissing clears `preview`, which stops the forward. A forward reopened on another
            // local port is a new item, so the sheet comes back with the page at the new address.
            .sheet(item: $model.preview) { preview in
                PreviewSheet(
                    url: preview.url, title: PortsCopy.previewTitle(preview.remote),
                    activity: { model.touch() }
                )
            }
    }

    @ViewBuilder private var content: some View {
        if !model.subject.canPorts {
            StateView(kind: .empty, title: PortsCopy.unavailable, message: ReviewCopy.unavailable)
        } else {
            switch model.phase {
            case .loading:
                StateView(kind: .loading, title: PortsCopy.loading, message: model.subject.title)
            case .failed(let message):
                StateView(
                    kind: .error, title: PortsCopy.loadFailed, message: message,
                    action: .init(ReviewCopy.tryAgain) { Task { await model.refresh() } }
                )
            case .loaded:
                // Ports arriving or leaving, and a port's forward opening or failing, glide on
                // `resize`; a refresh that changes nothing moves nothing (motion spec §2.5).
                IdentityKeyed(PortsShown(model), key: PortsShown.shape) { shown in
                    list(shown)
                }
            }
        }
    }

    private func list(_ shown: PortsShown) -> some View {
        List {
            Section {
                if shown.ports.isEmpty {
                    StateView(kind: .empty, title: PortsCopy.emptyTitle, message: PortsCopy.emptyMessage)
                        .padding(.vertical, 24)
                        .listRowBackground(Color.clear)
                } else {
                    ForEach(shown.ports, id: \.self) { port in
                        PortRow(
                            port: port, state: shown.states[port.port], note: shown.notes[port.port],
                            gone: shown.gone.contains(port.port),
                            open: { Task { await model.open(port) } },
                            copy: { Task { await model.copyLink(port) } },
                            stop: { Task { await model.close(port.port) } }
                        )
                        .groupedRows()
                    }
                }
            } footer: {
                Text(PortsCopy.footer)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
            }
            Section {
                ManualPortRow(model: model)
                    .groupedRows()
            } header: {
                SectionHeading(PortsCopy.manualTitle)
                    .textCase(nil)
            } footer: {
                Text(PortsCopy.manualFooter)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .groupedList()
        .refreshable { await model.refresh() }
    }
}

/// What the ports list draws, mirrored so a change of rows or of a row's lines is one `resize`
/// transaction.
struct PortsShown: Equatable {
    var ports: [ListeningPort]
    var states: [Int: ForwardState]
    var notes: [Int: String]
    var gone: Set<Int>

    @MainActor init(_ model: PortsModel) {
        ports = model.ports
        states = model.states
        notes = model.notes
        gone = model.gone
    }

    /// The rows, and per row which lines it shows; not the words in them.
    static func shape(_ shown: PortsShown) -> [String] {
        shown.ports.map { port in
            let state = shown.states[port.port].map { CaseName.of($0) } ?? "none"
            return "\(String(describing: port)) \(state) \(shown.notes[port.port] != nil) \(shown.gone.contains(port.port))"
        }
    }
}

/// "3000, node, pid 4121": the port in SF Mono, then what is listening, in words.
private struct PortRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let port: ListeningPort
    let state: ForwardState?
    let note: String?
    let gone: Bool
    let open: () -> Void
    let copy: () -> Void
    let stop: () -> Void

    private var previewable: Bool { PortsModel.isPreviewable(port) }

    var body: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 10))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            details
                .frame(maxWidth: .infinity, alignment: .leading)
            trailing
        }
        .padding(.vertical, 4)
        .contextMenu {
            if previewable {
                Button(PortsCopy.copyLink, systemImage: "link", action: copy)
            }
            if case .open? = state {
                Button(PortsCopy.stop, systemImage: "stop.circle", action: stop)
            }
        }
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 4) {
            title
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(PortsCopy.rowLabel(port))
            if let note = PortsModel.addressNote(port) {
                line(note)
            }
            if gone {
                line(PortsCopy.gone)
            }
            status
        }
    }

    private var title: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                number
                detailText
            }
            VStack(alignment: .leading, spacing: 2) {
                number
                detailText
            }
        }
    }

    private var number: some View {
        Text(verbatim: String(port.port))
            .font(.system(.body, design: .monospaced).weight(.semibold))
            .foregroundStyle(Color.togoSnow)
    }

    private var detailText: some View {
        Text(PortsCopy.detail(port))
            .font(.body)
            .foregroundStyle(Color.togoMute)
    }

    @ViewBuilder private var status: some View {
        switch state {
        case .open(let info)?:
            if !gone {
                Text(forwardedTo(info.localPort))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
            if let note { line(note, symbol: "exclamationmark.triangle") }
        case .failed(let message)?:
            line(message, symbol: "exclamationmark.triangle")
        case .opening?, .idle?, nil:
            if let note { line(note) }
        }
    }

    /// "Forwarded to 127.0.0.1:50000", the address in SF Mono.
    private func forwardedTo(_ localPort: Int) -> AttributedString {
        var address = AttributedString("127.0.0.1:\(localPort)")
        address.font = .system(.subheadline, design: .monospaced)
        return AttributedString(PortsCopy.forwardedTo) + address
    }

    private func line(_ text: String, symbol: String? = nil) -> some View {
        Label {
            Text(text)
                .font(.subheadline)
                .foregroundStyle(symbol == nil ? Color.togoMute : Color.togoSnow)
        } icon: {
            if let symbol {
                Image(systemName: symbol)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
            }
        }
        .labelStyle(NoteLabelStyle(hasIcon: symbol != nil))
    }

    /// One slot: the spinner and Open or Show crossfade on `respond`.
    private var trailing: some View {
        ZStack {
            if state == .opening {
                ProgressView()
                    .frame(minWidth: 44, minHeight: 44)
                    .accessibilityLabel(PortsCopy.opening)
                    .transition(.opacity)
            } else if previewable {
                let isOpen = if case .open? = state { true } else { false }
                Button(isOpen ? PortsCopy.show : PortsCopy.open, action: open)
                    .buttonStyle(.secondary(.fit))
                    .accessibilityLabel(isOpen ? PortsCopy.showLabel(port.port) : PortsCopy.openLabel(port.port))
                    .transition(.opacity)
            }
        }
        .motion(.respond, value: state.map { CaseName.of($0) })
    }
}

private struct NoteLabelStyle: LabelStyle {
    let hasIcon: Bool

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if hasIcon { configuration.icon.accessibilityHidden(true) }
            configuration.title
        }
    }
}

/// A port typed by hand, for a server this session didn't start.
private struct ManualPortRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var model: PortsModel
    @FocusState private var focused: Bool
    /// The error line as drawn: it arrives in a `resize` transaction.
    @State private var errorShown = LiveMirror<String?>()

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            let layout = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                : AnyLayout(HStackLayout(spacing: 12))
            layout {
                TextField(PortsCopy.manualPlaceholder, text: $model.manualText)
                    .font(.system(.body, design: .monospaced))
                    .keyboardType(.numberPad)
                    .textContentType(.none)
                    .autocorrectionDisabled()
                    .focused($focused)
                    .frame(minHeight: 44)
                    .accessibilityLabel(PortsCopy.manualTitle)
                    .onSubmit(submit)
                    // The number pad has no Return key.
                    .toolbar {
                        ToolbarItemGroup(placement: .keyboard) {
                            if focused {
                                Spacer()
                                Button(PortsCopy.open, action: submit)
                                    .disabled(model.manualText.trimmingCharacters(in: .whitespaces).isEmpty)
                            }
                        }
                    }
                Button(PortsCopy.open, action: submit)
                    .buttonStyle(.secondary(.fit))
                    .disabled(model.manualText.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            if let error = errorShown.read(model.manualError) {
                Label {
                    Text(error)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoSnow)
                } icon: {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.subheadline)
                        .foregroundStyle(Color.togoSnow)
                }
                .labelStyle(NoteLabelStyle(hasIcon: true))
                .transition(Motion.revealConceal(reduce: reduceMotion))
            }
        }
        .mirrors(model.manualError, into: $errorShown)
        .haptic(.error, trigger: model.manualError) { _, new in new != nil }
    }

    private func submit() {
        focused = false
        let text = model.manualText
        Task { await model.openManual(text) }
    }
}

/// The ports screen's sentences, under `review.ports.`.
enum PortsCopy {
    static let loading = String(localized: "review.ports.loading", defaultValue: "Looking for servers")
    static let loadFailed = String(localized: "review.ports.loadFailed", defaultValue: "Couldn't list the ports")
    static let unavailable = String(localized: "review.ports.unavailable", defaultValue: "Ports aren't available")
    static let emptyTitle = String(localized: "review.ports.empty", defaultValue: "No servers running")
    static let emptyMessage = String(
        localized: "review.ports.emptyMessage", defaultValue: "When this session starts a server, its port shows up here."
    )
    static let open = String(localized: "review.ports.open", defaultValue: "Open")
    static let opening = String(localized: "review.ports.opening", defaultValue: "Opening")
    static let show = String(localized: "review.ports.show", defaultValue: "Show")
    static let forwardedTo = String(localized: "review.ports.forwardedTo", defaultValue: "Forwarded to ")
    static let copyLink = String(localized: "review.ports.copyLink", defaultValue: "Copy link")
    static let stop = String(localized: "review.ports.stop", defaultValue: "Stop preview")
    static let gone = String(localized: "review.ports.gone", defaultValue: "Not listening any more")
    static let idleStopped = String(
        localized: "review.ports.idleStopped", defaultValue: "Stopped after 10 minutes without use."
    )
    static let done = String(localized: "review.ports.done", defaultValue: "Done")
    static let back = String(localized: "review.ports.back", defaultValue: "Back")
    static let forward = String(localized: "review.ports.forward", defaultValue: "Forward")
    static let reload = String(localized: "review.ports.reload", defaultValue: "Reload")
    static let pageFailed = String(localized: "review.ports.pageFailed", defaultValue: "The page didn't load")
    static let manualTitle = String(localized: "review.ports.manual", defaultValue: "Open another port")
    static let manualPlaceholder = String(localized: "review.ports.manualPlaceholder", defaultValue: "Port number")
    static let manualFooter = String(
        localized: "review.ports.manualFooter", defaultValue: "For a server this session didn't start."
    )
    static let manualRange = String(localized: "review.ports.manualRange", defaultValue: "Enter a port from 1 to 65535.")

    @MainActor static var footer: String {
        let device = UIDevice.current.userInterfaceIdiom == .pad
            ? String(
                localized: "review.ports.footer.pad",
                defaultValue: "Previews close when you leave this screen, switch apps, or after 10 minutes idle. Other apps on this iPad can reach a preview while it's open."
            )
            : String(
                localized: "review.ports.footer.phone",
                defaultValue: "Previews close when you leave this screen, switch apps, or after 10 minutes idle. Other apps on this iPhone can reach a preview while it's open."
            )
        return device
    }

    /// Digits never grouped, as a number would be.
    static func detail(_ port: ListeningPort) -> String {
        guard port.pid > 0 else {
            return String(localized: "review.ports.byNumber", defaultValue: "opened by number")
        }
        let command = port.command.isEmpty
            ? String(localized: "review.ports.unknownCommand", defaultValue: "unknown program")
            : DisplayText.clean(port.command)
        return String(localized: "review.ports.detail", defaultValue: "\(command), pid \(String(port.pid))")
    }

    static func rowLabel(_ port: ListeningPort) -> String {
        "\(String(port.port)), \(detail(port))"
    }

    static func notPreviewable(_ address: String) -> String {
        String(
            localized: "review.ports.notPreviewable",
            defaultValue: "Listening on \(address) only, so it can't be previewed from here."
        )
    }

    static func openLabel(_ port: Int) -> String {
        String(localized: "review.ports.openLabel", defaultValue: "Open \(String(port))")
    }

    static func showLabel(_ port: Int) -> String {
        String(localized: "review.ports.showLabel", defaultValue: "Show \(String(port))")
    }

    static func previewTitle(_ port: Int) -> String {
        String(localized: "review.ports.previewTitle", defaultValue: "Port \(String(port))")
    }
}

#if DEBUG
#Preview("Ports") {
    NavigationStack {
        PortsView(model: .fixture())
    }
}
#endif
