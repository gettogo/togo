import SwiftUI
import TogoKit

/// A saved machine's routes in its details: the one in use, every address Togo knows, and a
/// way to add or remove one typed by hand.
struct MachineRoutesSection: View {
    @Binding var draft: MachineDraft
    @State private var inUse: String?
    @State private var phoneOnTailnet = PhoneTailnet.isConnected()

    var body: some View {
        Section {
            if let shown = inUse ?? draft.lastRoute {
                LabeledContent {
                    Text(Self.place(shown, routes: draft.routes))
                        .foregroundStyle(Color.togoSnow)
                } label: {
                    Text(inUse == nil
                        ? String(localized: "routes.lastUsed", defaultValue: "Last used")
                        : String(localized: "routes.inUse", defaultValue: "In use"))
                        .foregroundStyle(Color.togoMute)
                }
                .frame(minHeight: 44)
            }
            ForEach(draft.routes) { route in
                row(route, current: route.id == (inUse ?? draft.lastRoute)?.lowercased())
            }
            addRow
        } header: {
            SectionHeading(String(localized: "routes.title", defaultValue: "Routes"))
        } footer: {
            Text(String(
                localized: "routes.footer",
                defaultValue: "Togo tries these together and uses the first one that answers with this machine's key."
            ))
        }
        .groupedRows()
        .task(id: draft.machineID) {
            guard let id = draft.machineID else { return }
            inUse = await RouteBook.shared.lastUse(id)?.route
        }
        if let away = AwayFromHome(
            machineOnTailnet: draft.routes.contains { $0.kind == .tailscale }, phoneOnTailnet: phoneOnTailnet
        ) {
            Section {
                AwayFromHomeNote(machine: draft.trimmedName, state: away)
                    .padding(.vertical, 4)
            }
            .groupedRows()
        }
    }

    static func place(_ host: String, routes: [HostRoute]) -> String {
        switch RoutePlan.place(of: host, in: routes) {
        case .tailscale: String(localized: "routes.place.tailscale", defaultValue: "Via Tailscale")
        case .localNetwork: String(localized: "routes.place.local", defaultValue: "On local network")
        case .address(let address): address
        }
    }

    private func row(_ route: HostRoute, current: Bool) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: route.host)
                    .font(.system(.body, design: .monospaced))
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .textSelection(.enabled)
                Text(Self.caption(route))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
            Spacer(minLength: 8)
            if current {
                Image(systemName: "checkmark")
                    .foregroundStyle(Color.togoSnow)
                    .accessibilityLabel(String(localized: "routes.current", defaultValue: "In use"))
            }
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
        .swipeActions {
            if draft.canRemove(route) {
                Button(String(localized: "routes.remove", defaultValue: "Remove"), systemImage: "trash", role: .destructive) {
                    draft.removeRoute(route)
                }
            }
        }
        .contextMenu {
            if draft.canRemove(route) {
                Button(String(localized: "routes.remove", defaultValue: "Remove"), systemImage: "trash", role: .destructive) {
                    draft.removeRoute(route)
                }
            }
        }
        .accessibilityActions {
            if draft.canRemove(route) {
                Button(String(localized: "routes.remove", defaultValue: "Remove")) { draft.removeRoute(route) }
            }
        }
    }

    private var addRow: some View {
        HStack(spacing: 12) {
            TextField(
                String(localized: "routes.add.field", defaultValue: "Add an address"),
                text: $draft.newRoute,
                prompt: Text(String(localized: "routes.add.prompt", defaultValue: "Add an address")).foregroundStyle(Color.togoMute)
            )
            .font(.body)
            .foregroundStyle(Color.togoSnow)
            .keyboardType(.asciiCapable)
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
            .submitLabel(.done)
            .onSubmit { draft.addRoute() }
            Button(String(localized: "routes.add", defaultValue: "Add"), systemImage: "plus") { draft.addRoute() }
                .buttonStyle(.borderless)
                .disabled(draft.newRouteHost == nil)
        }
        .frame(minHeight: 44)
    }

    static func caption(_ route: HostRoute) -> String {
        let kind = switch route.kind {
        case .tailscale: String(localized: "routes.kind.tailscale", defaultValue: "Tailscale")
        case .local, .lan: String(localized: "routes.kind.local", defaultValue: "Local network")
        case .other: String(localized: "routes.kind.other", defaultValue: "Other network")
        }
        guard route.manual else { return kind }
        return String(localized: "routes.kind.manual", defaultValue: "\(kind) · added by you")
    }
}
