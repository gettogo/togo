import SwiftUI
import TogoKit
import TogoTerminal

/// The keys behind the controller: two wide pills per row, followed by the user's shortcuts
/// (the agent's own first) and Edit keys. The contents scroll within the available popover.
struct KeysPaletteView: View {
    static let width: CGFloat = 232
    static let height: CGFloat = 228

    let config: TerminalKeysConfig
    let agent: String?
    let hidesInputTools: Bool
    let latch: ModifierLatch
    let hasMode: Bool
    let press: (KeyRef) -> Void
    let lock: (KeyModifiers, Bool) -> Void
    let perform: (KeyBarItem) -> Void
    let edit: () -> Void
    @State private var latchStates: [KeyModifiers: ModifierLatch.State] = [:]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 4) {
                LazyVGrid(columns: columns, spacing: 6) {
                    ForEach(keys, id: \.self) { key($0) }
                }
                let own = agent.flatMap { config.shortcutsByAgent[$0.lowercased()] } ?? []
                if !own.isEmpty {
                    header("\(KeysCopy.yours) · \(agent.map(AgentCatalog.name) ?? "")")
                    shortcutRows(own)
                }
                if !config.shortcuts.isEmpty {
                    header("\(KeysCopy.yours) · \(KeysCopy.everyAgent)")
                    shortcutRows(config.shortcuts)
                }
                Button(KeysCopy.editKeys, action: edit)
                    .buttonStyle(.plain)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, minHeight: 34)
                    .accessibilityIdentifier("keys.palette.edit")
            }
            .padding(6)
        }
        .frame(idealWidth: Self.width, idealHeight: Self.height)
        .onAppear(perform: readLatch)
        .accessibilityLabel(KeysCopy.palette)
    }

    private var columns: [GridItem] {
        [GridItem(.flexible(minimum: 0), spacing: 6), GridItem(.flexible(minimum: 0), spacing: 6)]
    }

    private func shortcutRows(_ items: [KeyBarItem]) -> some View {
        LazyVGrid(columns: columns, spacing: 6) {
            ForEach(items) { shortcut($0) }
        }
    }

    /// The palette's keys: arrows first while the controller is off; Mode and Stop first in
    /// compact height, where the column has no room for them.
    private var keys: [KeyRef] {
        var keys = config.palette
        if hidesInputTools { keys.removeAll { $0 == .attach || $0 == .dictate } }
        if !config.controller { keys = KeyRef.arrows + keys }
        return keys
    }

    private func header(_ text: String) -> some View {
        Text(text)
            .font(.caption2.weight(.semibold))
            .textCase(.uppercase)
            .foregroundStyle(Color.togoMuteDim)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 6)
            .padding(.top, 6)
    }

    @ViewBuilder private func key(_ ref: KeyRef) -> some View {
        if let modifier = ref.latch {
            let state = latchStates[modifier] ?? .off
            Button {
                press(ref)
                readLatch()
            } label: {
                face(ref)
            }
            // Armed is outlined, locked is filled: shape, not colour, says which (the accent is
            // kept for what needs the user).
            .buttonStyle(.borderedProminent)
            .tint(state == .locked ? Color.togoSnow : Color.togoRaised)
            .foregroundStyle(state == .locked ? Color.togoInk : Color.togoSnow)
            .overlay {
                if state == .once {
                    RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Color.togoSnow, lineWidth: 2)
                }
            }
            .accessibilityLabel(ref.title)
            .accessibilityValue(state == .off ? "Off" : state == .once ? "On for next key" : "Locked")
            .accessibilityAction(named: KeysCopy.lock) { lock(modifier, true); readLatch() }
            .accessibilityAction(named: KeysCopy.unlock) { lock(modifier, false); readLatch() }
        } else if ref == .mode, !hasMode {
            EmptyView()
        } else {
            Button { press(ref) } label: { face(ref) }
                .buttonStyle(.bordered)
                .tint(Color.togoRaised)
                .foregroundStyle(Color.togoSnow)
                .accessibilityLabel(ref.title)
        }
    }

    private func face(_ ref: KeyRef) -> some View {
        Group {
            if let symbol = ref.symbol {
                Image(systemName: symbol)
            } else {
                Text(ref.title)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .font(.subheadline.weight(.semibold))
        .frame(maxWidth: .infinity, minHeight: 36)
    }

    private func shortcut(_ item: KeyBarItem) -> some View {
        Button { perform(item) } label: {
            Group {
                if let symbol = item.symbol {
                    Image(systemName: symbol)
                } else {
                    Text(item.title)
                        .font(.footnote.monospaced())
                        .multilineTextAlignment(.center)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, minHeight: 36)
        }
        .buttonStyle(.bordered)
        .tint(Color.togoRaised)
        .foregroundStyle(Color.togoSnow)
        .accessibilityLabel(item.title)
    }

    private func readLatch() {
        latchStates = [.ctrl: latch.ctrl, .alt: latch.alt, .shift: latch.shift]
    }
}
