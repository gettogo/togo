import SwiftUI
import TogoKit

/// What it takes to reach a machine away from home, from what the machine and this device
/// say about Tailscale.
enum AwayFromHome: Equatable {
    /// Both are on a tailnet: nothing to do.
    case ready
    /// The machine has Tailscale and this device does not.
    case getTailscaleHere
    /// The machine has no Tailscale, so the steps are explained.
    case explain

    static let appStoreURL = URL(string: "https://apps.apple.com/app/tailscale/id1470499037")!

    /// Nil when the machine's helper could not say, so nothing is guessed.
    init?(machineOnTailnet: Bool?, phoneOnTailnet: Bool) {
        guard let machineOnTailnet else { return nil }
        switch (machineOnTailnet, phoneOnTailnet) {
        case (true, true): self = .ready
        case (true, false): self = .getTailscaleHere
        case (false, _): self = .explain
        }
    }
}

/// The Tailscale tip or the away-from-home steps, for the helper check and machine details.
struct AwayFromHomeNote: View {
    let machine: String
    let state: AwayFromHome
    @State private var stepsShown = false

    var body: some View {
        switch state {
        case .ready:
            ReadinessLine(
                text: String(localized: "routes.away.ready", defaultValue: "Reachable away from home over Tailscale"),
                ready: true
            )
        case .getTailscaleHere:
            VStack(alignment: .leading, spacing: 10) {
                Text(String(
                    localized: "routes.away.tip",
                    defaultValue: "\(machine) uses Tailscale. Get Tailscale on this \(DeviceName.model) and sign in with the same account to reach it away from home."
                ))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .fixedSize(horizontal: false, vertical: true)
                getTailscale
            }
        case .explain:
            DisclosureGroup(isExpanded: $stepsShown) {
                VStack(alignment: .leading, spacing: 12) {
                    step(1, String(
                        localized: "routes.away.step1",
                        defaultValue: "Install Tailscale on \(machine) and sign in."
                    ))
                    step(2, String(
                        localized: "routes.away.step2",
                        defaultValue: "Install Tailscale on this \(DeviceName.model) and sign in with the same account."
                    ))
                    step(3, String(
                        localized: "routes.away.step3",
                        defaultValue: "Connect to \(machine) once. Togo learns its Tailscale address and uses it whenever the home network can't be reached."
                    ))
                    Text(String(
                        localized: "routes.away.mosh",
                        defaultValue: "Mosh needs UDP ports 60000 to 61000. If a firewall on \(machine) blocks them, Togo uses SSH instead."
                    ))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
                    getTailscale
                }
                .padding(.top, 8)
            } label: {
                Label(
                    String(localized: "routes.away.title", defaultValue: "Use Togo away from home"),
                    systemImage: "globe"
                )
                .foregroundStyle(Color.togoSnow)
            }
            .tint(Color.togoMute)
        }
    }

    private var getTailscale: some View {
        Link(destination: AwayFromHome.appStoreURL) {
            Label(String(localized: "routes.away.get", defaultValue: "Get Tailscale"), systemImage: "arrow.down.app")
        }
        .buttonStyle(.secondary(.fit))
    }

    private func step(_ number: Int, _ text: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(verbatim: "\(number)")
                .font(.system(.subheadline, design: .monospaced))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
            Text(text)
                .font(.subheadline)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
    }
}
