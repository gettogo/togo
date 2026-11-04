#if DEBUG
import Observation
import SwiftUI
import TogoKit
import TogoTerminal

/// One attached session: the link keeps it attached, the binding moves bytes between the
/// link and the engine.
@MainActor @Observable final class DebugTerminalModel {
    let profile: HostProfile
    let session: Session
    let engine: SwiftTermEngine
    let controller: TerminalViewController
    var settings = TerminalSettings()
    private(set) var state: LinkState = .connecting
    private(set) var latestNote: FallbackNote?
    private(set) var resizesSent = 0

    @ObservationIgnored private var link: TerminalLink?
    @ObservationIgnored private var binding: TerminalBinding?
    @ObservationIgnored private var tasks: [Task<Void, Never>] = []

    init(profile: HostProfile, session: Session) {
        self.profile = profile
        self.session = session
        engine = SwiftTermEngine(settings: TerminalSettings(), theme: BuiltInThemes.all[0])
        controller = TerminalViewController(engine: engine)
        engine.inputAccessory = KeyBarView(config: .claude, engine: engine)
    }

    var header: String {
        var parts: [String] = []
        switch state {
        case .waking(let host): parts.append("Waking \(host)…")
        case .connecting: parts.append("Connecting…")
        case .live(let kind, _): parts.append(kind.displayName)
        case .roaming: parts.append("Roaming")
        case .reconnecting(let attempt): parts.append("Reconnecting, attempt \(attempt)")
        case .failed(let reason): parts.append(reason)
        case .ended: parts.append("This session has ended.")
        }
        if let latestNote { parts.append("\(latestNote.kind.displayName) skipped: \(latestNote.reason)") }
        // The copy rules allow one "·" per line, so it only sets off the counter.
        return parts.joined(separator: ", ") + " · Resizes sent: \(resizesSent)"
    }

    /// True from the first sign of a reconnect until live again, including a wake probe.
    var isReconnecting: Bool {
        switch state {
        case .reconnecting, .waking, .connecting: true
        case .live, .roaming, .failed, .ended: false
        }
    }

    func start() {
        guard link == nil else { return }
        let size = engine.size
        let link = TerminalLink(
            connect: SessionAttach.connect(profile: profile, sessionID: session.id, env: DebugComposition.shared.env),
            path: SystemPathSource(),
            cols: size.cols > 0 ? size.cols : 80, rows: size.rows > 0 ? size.rows : 24
        )
        let binding = TerminalBinding(
            engine: engine, ticker: DisplayLinkTicker(),
            read: { await link.read() },
            send: { await link.send($0) },
            resize: { cols, rows in await link.resize(cols: cols, rows: rows) }
        )
        self.link = link
        self.binding = binding
        tasks.append(Task { [weak self] in
            for await state in link.states { self?.apply(state) }
        })
        // The link coalesces resizes, so the count is read back rather than counted here.
        tasks.append(Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(250))
                let sent = await link.resizesSent
                self?.resizesSent = sent
            }
        })
        binding.start()
        Task { await link.start() }
    }

    func stop() {
        binding?.stop()
        tasks.forEach { $0.cancel() }
        tasks = []
        if let link { Task { await link.stop() } }
        link = nil
        binding = nil
    }

    func scenePhaseChanged(to phase: ScenePhase) {
        guard let link else { return }
        switch phase {
        case .background: Task { await link.didEnterBackground() }
        case .active: Task { await link.willEnterForeground() }
        default: break
        }
    }

    func copyLastLines() async -> String {
        let id = session.id
        do {
            let result = try await DebugComposition.shared.withHelper(profile) { helper in
                try await helper.capture(id: id, lines: 500, join: true)
            }
            UIPasteboard.general.string = result.text
            return result.truncated ? "Copied the last 500 lines." : "Copied the whole history."
        } catch {
            return error.localizedDescription
        }
    }

    func apply(theme: TerminalTheme) {
        settings.themeID = theme.id
        engine.apply(theme)
    }

    func apply(fontName: String?) {
        settings.fontName = fontName
        engine.apply(settings)
    }

    private func apply(_ state: LinkState) {
        self.state = state
        if case .live(_, let notes) = state { latestNote = notes.last }
    }
}

struct DebugTerminalScreen: View {
    @State private var model: DebugTerminalModel
    @State private var finding = false
    @State private var findText = ""
    @State private var message: String?
    @Environment(\.scenePhase) private var scenePhase

    init(profile: HostProfile, session: Session) {
        _model = State(initialValue: DebugTerminalModel(profile: profile, session: session))
    }

    var body: some View {
        VStack(spacing: 0) {
            Text(message ?? model.header)
                .font(.caption.monospaced())
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal)
                .padding(.vertical, 4)
            TerminalHost(controller: model.controller)
                .opacity(model.isReconnecting ? 0.6 : 1)
        }
        .navigationTitle(model.session.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .primaryAction) { actions } }
        .alert("Find", isPresented: $finding) {
            TextField("Text", text: $findText)
            Button("Find") {
                if !model.engine.find(findText, caseSensitive: false, regex: false, backwards: false) {
                    message = "No match for \(findText)"
                }
            }
            Button("Cancel", role: .cancel) { model.engine.clearFind() }
        }
        .onAppear { model.start() }
        .onDisappear { model.stop() }
        .onChange(of: scenePhase) { _, phase in model.scenePhaseChanged(to: phase) }
        .task(id: message) {
            guard message != nil else { return }
            try? await Task.sleep(for: .seconds(3))
            message = nil
        }
    }

    private var actions: some View {
        Menu {
            Button("Copy last 500 lines") { Task { message = await model.copyLastLines() } }
            Button("Find") { finding = true }
            Picker("Theme", selection: Binding(
                get: { model.settings.themeID },
                set: { id in BuiltInThemes.all.first { $0.id == id }.map(model.apply(theme:)) }
            )) {
                ForEach(BuiltInThemes.all) { Text($0.name).tag($0.id) }
            }
            .pickerStyle(.menu)
            Picker("Font", selection: Binding(
                get: { model.settings.fontName ?? "" },
                set: { model.apply(fontName: $0.isEmpty ? nil : $0) }
            )) {
                Text("System").tag("")
                ForEach(FontLibrary.installedFonts(), id: \.self) { Text($0).tag($0) }
            }
            .pickerStyle(.menu)
        } label: {
            Label("Actions", systemImage: "ellipsis.circle")
        }
    }
}

/// Hosts a TogoTerminal view controller in SwiftUI.
struct TerminalHost: UIViewControllerRepresentable {
    let controller: UIViewController

    func makeUIViewController(context: Context) -> UIViewController { controller }
    func updateUIViewController(_ controller: UIViewController, context: Context) {}
}
#endif
