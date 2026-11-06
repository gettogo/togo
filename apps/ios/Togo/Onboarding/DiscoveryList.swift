import SwiftUI
import TogoKit
import UIKit

/// Machines answering for SSH nearby, and the way to type an address instead.
struct DiscoveryList: View {
    @Environment(\.openURL) private var openURL
    let hosts: [DiscoveredHost]
    let phase: DiscoveryPhase
    let onChoose: (DiscoveredHost) -> Void
    let onEnterAddress: () -> Void

    var body: some View {
        Group {
            if phase == .denied {
                denied
            } else {
                list
            }
        }
        .background(Color.togoInk)
        .navigationTitle(String(localized: "onboard.discovery.title", defaultValue: "Add a machine"))
        .navigationBarTitleDisplayMode(.inline)
    }

    private var list: some View {
        List {
            Section {
                if hosts.isEmpty {
                    emptyRow
                } else {
                    ForEach(hosts) { host in
                        Button { onChoose(host) } label: { HostRow(host: host) }
                            .listRowBackground(Color.togoInk)
                    }
                }
            } header: {
                SectionHeading(String(localized: "onboard.discovery.nearby", defaultValue: "Nearby"))
                    .padding(.top, 8)
            }
            Section {
                Button(action: onEnterAddress) {
                    Label(
                        String(localized: "onboard.discovery.enterAddress", defaultValue: "Enter an address"),
                        systemImage: "keyboard"
                    )
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .contentShape(.rect)
                }
                .listRowBackground(Color.togoInk)
                Text(String(
                    localized: "onboard.discovery.footer",
                    defaultValue: "Any Mac or Linux machine you can reach over SSH works, including over Tailscale."
                ))
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .listRowBackground(Color.togoInk)
                .listRowSeparator(.hidden)
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .listRowSeparatorTint(Color.togoLine)
        .listSectionSpacing(28)
        .motion(.fade, value: hosts)
    }

    @ViewBuilder private var emptyRow: some View {
        Group {
            if phase == .quiet {
                Text(String(localized: "onboard.discovery.empty", defaultValue: "No machines found nearby. Enter an address."))
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
            } else {
                PawWalk(String(localized: "onboard.discovery.searching", defaultValue: "Looking for machines nearby"))
            }
        }
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .listRowBackground(Color.togoInk)
        .listRowSeparator(.hidden)
    }

    private var denied: some View {
        VStack(spacing: 0) {
            StateView(
                kind: .permission,
                title: String(
                    localized: "onboard.discovery.denied.title", defaultValue: "Togo can't look for machines on this network."
                ),
                message: String(
                    localized: "onboard.discovery.denied.message",
                    defaultValue: "Turn on Local Network for Togo in Settings, or type the machine's address."
                ),
                action: .init(String(localized: "onboard.discovery.openSettings", defaultValue: "Open settings")) {
                    if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                }
            )
            Button(String(localized: "onboard.discovery.enterAddress", defaultValue: "Enter an address"), action: onEnterAddress)
                .buttonStyle(.secondary)
                .padding(.horizontal, Layout.margin)
                .padding(.bottom, 16)
        }
    }
}

private struct HostRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let host: DiscoveredHost

    var body: some View {
        HStack(spacing: 14) {
            if !dynamicTypeSize.isAccessibilitySize {
                Image(systemName: host.name.localizedStandardContains("MacBook") ? "laptopcomputer" : "desktopcomputer")
                    .font(.title3)
                    .foregroundStyle(Color.togoMute)
                    .frame(width: 32)
                    .accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: host.name)
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
                Text(verbatim: detail)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
            Spacer(minLength: 8)
            Image(systemName: "chevron.forward")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
        }
        .frame(minHeight: 44)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    /// "192.168.1.20" or "100.101.102.103, Tailscale". The port shows only when unusual.
    private var detail: String {
        // Found by name only: the form asks for the address.
        if host.host.isEmpty {
            return String(localized: "onboard.discovery.noAddress", defaultValue: "Type its address on the next screen")
        }
        let port = String(host.port)
        let address = host.port == 22
            ? host.host
            : String(localized: "onboard.discovery.hostPort", defaultValue: "\(host.host) port \(port)")
        guard host.tailnet else { return address }
        return String(localized: "onboard.discovery.tailscale", defaultValue: "\(address), Tailscale")
    }
}
