import SwiftUI
import TogoKit

/// What the board needs beyond the store: the idle timer and battery for "Keep screen on",
/// and the thumbnails, which tests and snapshots fill in themselves.
struct BoardServices {
    var idleTimer: any IdleTimerControl
    var battery: any BatterySource
    /// Nil captures through the store's services.
    var thumbnails: ThumbnailModel?

    @MainActor static func live() -> BoardServices {
        BoardServices(idleTimer: WindowIdleTimer(), battery: DeviceBattery(), thumbnails: nil)
    }
}

/// Every live session as a card, grouped by what it needs from the user, each with the last
/// lines of its screen. With "Keep screen on" it stays up as a status display.
struct BoardView: View {
    @Environment(\.scenePhase) private var scenePhase
    @Bindable var store: AppStore
    /// Nil uses the device's own, made once for this board.
    private let services: BoardServices?
    private let givenMonitor: MonitorMode?
    @State private var models = BoardModels()
    @State private var appeared = false
    @State private var visible: Set<TabID> = []
    @State private var killTarget: SessionRow?
    @State private var renameTarget: SessionRow?

    init(store: AppStore, services: BoardServices? = nil, monitor: MonitorMode? = nil) {
        self.store = store
        self.services = services
        givenMonitor = monitor
    }

    private var thumbnails: ThumbnailModel {
        models.thumbnails(services: services, store: store)
    }

    private var monitor: MonitorMode {
        models.monitor(givenMonitor, services: services, store: store)
    }

    var body: some View {
        BoardGrid(
            sections: sections, note: monitor.note,
            onVisible: { id, isVisible in
                if isVisible { visible.insert(id) } else { visible.remove(id) }
            },
            card: cardButton
        )
        .offset(monitor.shift)
        .motion(.dim, value: monitor.shift)
        .overlay { dimmer }
        .simultaneousGesture(SpatialTapGesture().onEnded { _ in monitor.touch() })
        .onScrollPhaseChange { _, phase in
            if phase != .idle { monitor.touch() }
        }
        .background(Color.togoInk)
        .navigationTitle(BoardCopy.title)
        .toolbar { keepScreenOn }
        .modifier(SessionDialogs(store: store, kill: $killTarget, rename: $renameTarget))
        .readsFold()
        .toolbarVisibility(monitor.dimmed ? .hidden : .automatic, for: .navigationBar)
        .onAppear { appeared = true }
        // Let go here rather than through `onScreen`, whose change may never run for a board that
        // is leaving.
        .onDisappear {
            appeared = false
            monitor.setBoardVisible(false)
            thumbnails.setActive(false)
        }
        .onChange(of: store.services.settings.keepScreenOn) { _, on in
            if monitor.enabled != on { monitor.enabled = on }
        }
        .onChange(of: onScreen, initial: true) { _, shown in monitor.setBoardVisible(shown) }
        .onChange(of: scenePhase == .active, initial: true) { _, active in monitor.setSceneActive(active) }
        .onChange(of: polling, initial: true) { _, active in thumbnails.setActive(active) }
        .onChange(of: Board.onBoard(visible, sections: sections), initial: true) { _, keys in
            if keys != visible { visible = keys }
            thumbnails.setVisible(keys)
        }
        .onChange(of: needingYou, initial: true) { _, ids in monitor.needsYou(ids) }
        .task(id: polling) {
            while polling, !Task.isCancelled {
                thumbnails.pump(at: Date())
                try? await Task.sleep(for: .milliseconds(500))
            }
        }
        .task(id: watching) {
            while watching, !Task.isCancelled {
                monitor.batteryChanged()
                monitor.tick()
                try? await Task.sleep(for: .seconds(15))
            }
        }
    }

    // MARK: State

    private var sections: [BoardSection] {
        Board.sections(machines: store.controlCenter.machines)
    }

    private var needingYou: Set<TabID> {
        Set(sections.first { $0.kind == .needsYou }?.cards.map(\.id) ?? [])
    }

    /// In a compact window the workspace covers the board while it is open.
    private var onScreen: Bool {
        appeared && !(store.workspaceOpen && !store.workspaceInline)
    }

    /// Thumbnails fetch only while the board is on screen in an active scene.
    private var polling: Bool {
        onScreen && scenePhase == .active
    }

    /// The monitor's clock runs while it could hold the screen, so it notices a charger too.
    private var watching: Bool {
        monitor.enabled && polling
    }

    // MARK: Cards

    private func cardButton(_ card: BoardCard) -> some View {
        let row = card.row
        let actions = SessionActions.actions(for: row, on: store.machine(for: row.hostID))
        return Button {
            store.openFromBoard(row)
        } label: {
            SessionCard(
                card: card, thumbnail: thumbnails.text(for: card.id), capturing: thumbnails.supportsCapture(card.id),
                shown: store.visibleTabs.contains(card.id)
            )
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .contextMenu {
            SessionMenu(row: row, actions: actions, etAvailable: store.etAvailable(row.hostID), perform: perform) {
                if store.canOpenBesideFromBoard(card.id) {
                    Button(AppCommandCopy.openBeside, systemImage: "rectangle.split.2x1") { store.openBesideFromBoard(row) }
                }
                OpenInWindowButton(tab: card.id)
            }
        }
    }

    private func perform(_ command: SessionCommand) {
        store.run(
            command, open: { row, transport in store.openFromBoard(row, transport: transport) },
            kill: { killTarget = $0 }, rename: { renameTarget = $0 }
        )
    }

    // MARK: Keep screen on

    @ToolbarContentBuilder private var keepScreenOn: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Toggle(isOn: keepScreenOnChoice) {
                Label(BoardCopy.keepScreenOn, systemImage: monitor.enabled ? "sun.max.fill" : "sun.max")
            }
            .toggleStyle(.button)
            .foregroundStyle(Color.togoSnow)
        }
    }

    private var keepScreenOnChoice: Binding<Bool> {
        Binding {
            monitor.enabled
        } set: { on in
            monitor.enabled = on
            monitor.touch()
            store.services.settings.keepScreenOn = on
        }
    }

    /// Black at 70 % over the board after ten minutes alone. A tap lifts it and does nothing else.
    private var dimmer: some View {
        Color.black
            .opacity(monitor.dimmed ? MonitorMode.dimOpacity : 0)
            .ignoresSafeArea()
            .allowsHitTesting(monitor.dimmed)
            .onTapGesture { monitor.touch() }
            .accessibilityElement()
            .accessibilityLabel(BoardCopy.wake)
            .accessibilityAddTraits(.isButton)
            .accessibilityHidden(!monitor.dimmed)
            .accessibilityAction { monitor.touch() }
            .motion(.dim, value: monitor.dimmed)
    }
}

/// The board's models, made the first time the board asks for them rather than on every
/// rebuild of the view that shows the board.
@MainActor private final class BoardModels {
    private var thumbnailModel: ThumbnailModel?
    private var monitorMode: MonitorMode?
    private var live: BoardServices?

    func thumbnails(services: BoardServices?, store: AppStore) -> ThumbnailModel {
        if let thumbnailModel { return thumbnailModel }
        let made = services?.thumbnails ?? ThumbnailModel(store: store)
        thumbnailModel = made
        return made
    }

    func monitor(_ given: MonitorMode?, services: BoardServices?, store: AppStore) -> MonitorMode {
        if let monitorMode { return monitorMode }
        let parts = services ?? liveServices()
        let made = given ?? MonitorMode(
            enabled: store.services.settings.keepScreenOn, idleTimer: parts.idleTimer, battery: parts.battery
        )
        monitorMode = made
        return made
    }

    private func liveServices() -> BoardServices {
        if let live { return live }
        let made = BoardServices.live()
        live = made
        return made
    }
}

/// The sections and their cards in rows, laid out by `BoardLayout`.
private struct BoardGrid<Card: View>: View {
    @Environment(\.fold) private var fold
    let sections: [BoardSection]
    let note: String?
    let onVisible: (TabID, Bool) -> Void
    @ViewBuilder let card: (BoardCard) -> Card

    var body: some View {
        GeometryReader { proxy in
            ScrollView {
                content(BoardLayout.grid(width: proxy.size.width, fold: fold))
            }
        }
        .overlay {
            if sections.isEmpty {
                StateView(kind: .empty, title: BoardCopy.empty, message: BoardCopy.emptyMessage)
            }
        }
    }

    private func content(_ grid: BoardLayout.Grid) -> some View {
        LazyVStack(alignment: .leading, spacing: BoardLayout.spacing) {
            if let note {
                Label(note, systemImage: "battery.25percent")
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .padding(.horizontal, BoardLayout.margin)
            }
            ForEach(sections) { section in
                SectionHeading(BoardCopy.section(section.kind), detail: "\(section.cards.count)")
                    .padding(.leading, grid.columns.first?.minX ?? BoardLayout.margin)
                    .padding(.trailing, BoardLayout.margin)
                    .padding(.top, 8)
                ForEach(rows(section.cards, columns: grid.columns.count), id: \.first?.id) { cards in
                    row(cards, grid: grid)
                }
            }
        }
        .padding(.vertical, 12)
    }

    /// Cards of one row share its height; the gaps match the grid's, the fold's included.
    private func row(_ cards: [BoardCard], grid: BoardLayout.Grid) -> some View {
        HStack(alignment: .top, spacing: 0) {
            ForEach(Array(cards.enumerated()), id: \.element.id) { index, item in
                let column = grid.columns[index]
                let previous = index == 0 ? 0 : grid.columns[index - 1].maxX
                Color.clear.frame(width: max(0, column.minX - previous), height: 0)
                card(item)
                    .frame(width: column.width)
                    .frame(maxHeight: .infinity)
                    .onScrollVisibilityChange(threshold: 0.05) { onVisible(item.id, $0) }
            }
            Spacer(minLength: 0)
        }
        .fixedSize(horizontal: false, vertical: true)
    }

    private func rows(_ cards: [BoardCard], columns: Int) -> [[BoardCard]] {
        stride(from: 0, to: cards.count, by: max(1, columns)).map { start in
            Array(cards[start..<min(start + columns, cards.count)])
        }
    }
}

extension AppStore {
    /// ⌃⌘B. In a compact window the board comes up from under the workspace.
    func toggleBoard() {
        if boardShown {
            boardShown = false
            return
        }
        boardShown = true
        path = []
        if !workspaceInline {
            workspaceOpen = false
        }
    }

    /// A tap on a card: the session in the focused pane, or in a compact window the workspace
    /// over the board. Never a second pane.
    func openFromBoard(_ row: SessionRow, transport: TransportPreference? = nil) {
        guard workspaceInline else {
            open(hostID: row.hostID, sessionID: row.id, transport: transport)
            return
        }
        showFromSidebar(row, transport: transport)
        boardShown = false
    }

    /// Open Beside is offered beside a sidebar wide enough for two panes, for a card that is not
    /// already the primary pane's.
    func canOpenBesideFromBoard(_ id: TabID) -> Bool {
        workspaceInline && panesFit && reviewing == nil && workspace.selected != nil && workspace.selected != id
    }

    /// Open Beside from a card's menu: the one way the board puts a session in a second pane.
    func openBesideFromBoard(_ row: SessionRow) {
        openBeside(TabID(hostID: row.hostID, sessionID: row.id))
        boardShown = false
        workspaceOpen = true
    }
}

/// The List or Board choice, in the sidebar's toolbar and in the control center's menu.
struct BoardChoiceMenu: View {
    @Bindable var store: AppStore

    var body: some View {
        Menu {
            Picker(BoardCopy.view, selection: $store.boardShown) {
                Label(BoardCopy.list, systemImage: "list.bullet").tag(false)
                Label(BoardCopy.title, systemImage: "square.grid.2x2").tag(true)
            }
        } label: {
            Label(BoardCopy.view, systemImage: store.boardShown ? "square.grid.2x2" : "list.bullet")
        }
        .foregroundStyle(Color.togoSnow)
    }
}

#if DEBUG
#Preview("Board") {
    NavigationStack {
        BoardView(store: AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter))
    }
}
#endif
