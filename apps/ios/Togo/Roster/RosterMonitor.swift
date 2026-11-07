import SwiftUI
import TogoKit

/// What monitor mode needs beyond the store: the idle timer and battery for "Keep screen on".
struct MonitorServices {
    var idleTimer: any IdleTimerControl
    var battery: any BatterySource

    @MainActor static func live() -> MonitorServices {
        MonitorServices(idleTimer: WindowIdleTimer(), battery: DeviceBattery())
    }
}

/// The roster full screen as a status display. With "Keep screen on" the screen stays awake
/// while it is up; after ten minutes alone it dims and shifts a little, and a touch or a session
/// that newly needs the user lifts the dim.
struct RosterMonitor: View {
    @Environment(\.scenePhase) private var scenePhase
    @Bindable var store: AppStore
    /// Nil uses the device's own, made once for this screen.
    private let services: MonitorServices?
    private let givenMonitor: MonitorMode?
    var now: Date?
    @State private var models = MonitorModels()
    @State private var appeared = false

    init(store: AppStore, services: MonitorServices? = nil, monitor: MonitorMode? = nil, now: Date? = nil) {
        self.store = store
        self.services = services
        givenMonitor = monitor
        self.now = now
    }

    private var monitor: MonitorMode {
        models.monitor(givenMonitor, services: services, store: store)
    }

    var body: some View {
        VStack(spacing: 0) {
            if let note = monitor.note {
                Label(note, systemImage: "battery.25percent")
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 8)
            }
            RosterView(store: store, wide: store.workspaceInline, now: now)
        }
        .offset(monitor.shift)
        .motion(.dim, value: monitor.shift)
        .overlay { dimmer }
        .simultaneousGesture(SpatialTapGesture().onEnded { _ in monitor.touch() })
        .onScrollPhaseChange { _, phase in
            if phase != .idle { monitor.touch() }
        }
        .background(Color.togoInk)
        .toolbar { keepScreenOn }
        .toolbarVisibility(monitor.dimmed ? .hidden : .automatic, for: .navigationBar)
        .onAppear { appeared = true }
        // Let go here rather than through `onScreen`, whose change may never run for a screen
        // that is leaving.
        .onDisappear {
            appeared = false
            monitor.setBoardVisible(false)
        }
        .onChange(of: store.services.settings.keepScreenOn) { _, on in
            if monitor.enabled != on { monitor.enabled = on }
        }
        .onChange(of: polling, initial: true) { _, shown in monitor.setBoardVisible(shown) }
        .onChange(of: scenePhase == .active, initial: true) { _, active in monitor.setSceneActive(active) }
        .onChange(of: needingYou, initial: true) { _, ids in monitor.needsYou(ids) }
        .task(id: watching) {
            while watching, !Task.isCancelled {
                monitor.batteryChanged()
                monitor.tick()
                try? await Task.sleep(for: .seconds(15))
            }
        }
    }

    private var needingYou: Set<TabID> {
        Set(store.roster.lines.filter(\.needsYou).map(\.id))
    }

    /// On screen in an active scene; in a compact window the terminal covers it while open.
    private var polling: Bool {
        appeared && !(store.workspaceOpen && !store.workspaceInline)
    }

    /// The monitor's clock runs while it could hold the screen, so it notices a charger too.
    private var watching: Bool {
        monitor.enabled && polling && scenePhase == .active
    }

    @ToolbarContentBuilder private var keepScreenOn: some ToolbarContent {
        // The way out by touch; the shortcut is ⌃⌘B.
        ToolbarItem(placement: .topBarLeading) {
            Button(MonitorCopy.done, systemImage: "checkmark", role: .confirm) { store.toggleMonitor() }
                .foregroundStyle(Color.togoSnow)
                .accessibilityIdentifier("monitor.done")
        }
        ToolbarItem(placement: .topBarLeading) {
            Toggle(isOn: keepScreenOnChoice) {
                Label(MonitorCopy.keepScreenOn, systemImage: monitor.enabled ? "sun.max.fill" : "sun.max")
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

    /// Black at 70 % over the roster after ten minutes alone. A tap lifts it and does nothing else.
    private var dimmer: some View {
        Color.black
            .opacity(monitor.dimmed ? MonitorMode.dimOpacity : 0)
            .ignoresSafeArea()
            .allowsHitTesting(monitor.dimmed)
            .onTapGesture { monitor.touch() }
            .accessibilityElement()
            .accessibilityLabel(MonitorCopy.wake)
            .accessibilityAddTraits(.isButton)
            .accessibilityHidden(!monitor.dimmed)
            .accessibilityAction { monitor.touch() }
            .motion(.dim, value: monitor.dimmed)
    }
}

/// The monitor, made the first time the screen asks for it rather than on every rebuild.
@MainActor private final class MonitorModels {
    private var monitorMode: MonitorMode?

    func monitor(_ given: MonitorMode?, services: MonitorServices?, store: AppStore) -> MonitorMode {
        if let monitorMode { return monitorMode }
        let parts = services ?? MonitorServices.live()
        let made = given ?? MonitorMode(
            enabled: store.services.settings.keepScreenOn, idleTimer: parts.idleTimer, battery: parts.battery
        )
        monitorMode = made
        return made
    }
}

extension AppStore {
    /// ⌃⌘B: the roster full screen as a status display, or back. In a compact window it comes up
    /// from under the terminal.
    func toggleMonitor() {
        if monitorShown {
            monitorShown = false
            return
        }
        monitorShown = true
        path = []
        if !workspaceInline {
            workspaceOpen = false
        }
    }
}

/// Every sentence monitor mode shows. The keep-awake lines keep the keys they had.
enum MonitorCopy {
    static let toggle = String(localized: "monitor.cmd.toggle", defaultValue: "Monitor")
    static let done = String(localized: "monitor.done", defaultValue: "Done")
    static let keepScreenOn = String(localized: "board.keepScreenOn", defaultValue: "Keep screen on")
    static let lowBattery = String(
        localized: "board.lowBattery", defaultValue: "Screen can sleep again: battery under 20 %. Plug in to keep it on."
    )
    static let wake = String(localized: "monitor.wake", defaultValue: "Wake the screen")
}
