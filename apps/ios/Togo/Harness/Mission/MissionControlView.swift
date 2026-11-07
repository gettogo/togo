import SwiftUI
import TogoKit
import UIKit

/// Where mission control pushes inside its own stack.
enum MissionRoute: Hashable {
    /// A subagent's own conversation, by its agent item id.
    case agent(String)
    /// A background job's live output.
    case job(HarnessRow)
    /// A native screen, such as omp's Compaction settings from the status section.
    case screen(String)
}

/// Mission control: the session's sections in their fixed order (needs you, agents,
/// background, plan, changes, ports, status), each shown only when the harness supports it.
/// A sheet with medium and large detents on compact widths; a side inspector on iPad and on
/// an unfolded Duo. Agents, jobs and screens push in its own stack; diffs and previews go to
/// `context.open`, which the host routes to the review.
struct MissionControlView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: HarnessSessionStore
    let context: PrimitiveContext
    var actions: ChatActions = .inert
    /// Pushes a route onto the host's stack.
    var push: (MissionRoute) -> Void = { _ in }
    /// The user wants the chat itself, for a need mission control cannot answer here: scrolled
    /// to that request's card (its item id) when known, else to the oldest open one.
    var showChat: (String?) -> Void = { _ in }

    private var sections: [HarnessSection] {
        MissionLayout.sections(store.mission.mission, capabilities: store.descriptor.map { Set($0.capabilities) })
    }

    var body: some View {
        // Rows and sections arriving or leaving move in one `resize` transaction (`IdentityKeyed`);
        // text inside them updates in place. The first render is a cut.
        IdentityKeyed(sections, key: MissionLayout.shape) { sections in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 26) {
                    ForEach(sections) { section in
                        VStack(alignment: .leading, spacing: 10) {
                            MissionSectionHeader(kind: section.kind, count: count(section))
                            content(section)
                        }
                        .id(section.kind.rawValue)
                        // motion: preventing a jump — a section that arrives fades in where the
                        // sections below glided to make room; nothing rises.
                        .transition(Motion.arrive(reduce: reduceMotion))
                    }
                }
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 24)
            }
            .overlay { placeholder(empty: sections.isEmpty) }
        }
        .background(Color.togoInk)
        .navigationTitle(MissionCopy.title)
        .navigationBarTitleDisplayMode(.inline)
    }

    private func count(_ section: HarnessSection) -> Int {
        switch section.kind {
        case .needs: max(section.rows.count, store.mission.summary.needsYou)
        case .plan: 0
        default: section.rows.count
        }
    }

    // MARK: Sections

    @ViewBuilder private func content(_ section: HarnessSection) -> some View {
        switch section.kind {
        case .plan:
            if section.rows.isEmpty {
                MissionEmptyLine(text: MissionCopy.empty(.plan))
                    // motion: state change — the empty line and the plan swap in one slot, in place.
                    .transition(Motion.swap(reduce: reduceMotion))
            } else {
                ChecklistPrimitive(
                    checklist: MissionLayout.checklist(section.rows, summary: store.mission.summary.plan),
                    context: context, embedded: true
                )
                // motion: state change — the plan takes the empty line's slot, crossfading in place.
                .transition(Motion.swap(reduce: reduceMotion))
            }
        case .status:
            statusSection(section)
        default:
            if section.rows.isEmpty {
                MissionEmptyLine(text: MissionCopy.empty(section.kind))
                    // motion: state change — the empty line and the rows swap in one slot, in place.
                    .transition(Motion.swap(reduce: reduceMotion))
            } else {
                MissionCard {
                    ForEach(section.rows) { row in
                        rowView(row, kind: section.kind)
                    }
                }
                // motion: state change — the rows take the empty line's slot, crossfading in place.
                .transition(Motion.swap(reduce: reduceMotion))
            }
        }
    }

    @ViewBuilder private func rowView(_ row: HarnessRow, kind: MissionSectionKind) -> some View {
        switch kind {
        case .needs:
            MissionNeedsRow(row: row, store: store, actions: actions, showTerminal: { context.showTerminal() }) {
                showChat(MissionLayout.needsItem(for: row, in: store.timeline)?.id)
            }
        case .background:
            Button { open(row, kind: kind) } label: { MissionBackgroundRow(row: row) }
                .buttonStyle(.plain)
                .contextMenu { menu(row, kind: kind) }
        default:
            let target = MissionLayout.target(for: row, in: kind)
            Button { open(row, kind: kind) } label: {
                MissionRowView(row: row, opens: target != nil, openLabel: target.map(openLabel))
            }
            .buttonStyle(.plain)
            .disabled(target == nil)
            .contextMenu { menu(row, kind: kind) }
        }
    }

    /// The Status block, Compact on its context meter; before the first status arrives, the
    /// honest line saying when it will. Then any real pages the adapter lists here (never a
    /// second copy of the block, never a page holding one Confirm button).
    @ViewBuilder private func statusSection(_ section: HarnessSection) -> some View {
        let screens = MissionLayout.statusScreens(store.descriptor)
        VStack(alignment: .leading, spacing: 12) {
            if let status = section.status {
                StatusPrimitive(status: status, context: context, embedded: true)
                    // motion: state change — the block takes the waiting line's slot, in place.
                    .transition(Motion.swap(reduce: reduceMotion))
            } else {
                MissionEmptyLine(text: MissionCopy.statusPending(store.descriptor?.adapter?.name))
                    // motion: state change — the waiting line gives its slot to the block, in place.
                    .transition(Motion.swap(reduce: reduceMotion))
            }
            if !screens.isEmpty {
                MissionCard {
                    ForEach(screens) { screen in
                        Button { push(.screen(screen.id)) } label: {
                            HStack(spacing: 12) {
                                Text(screen.title)
                                    .font(.body)
                                    .foregroundStyle(Color.togoSnow)
                                    .fixedSize(horizontal: false, vertical: true)
                                Spacer(minLength: 8)
                                Image(systemName: "chevron.right")
                                    .font(.footnote.weight(.semibold))
                                    .foregroundStyle(Color.togoMuteDim)
                                    .accessibilityHidden(true)
                            }
                            .padding(.horizontal, 14)
                            .padding(.vertical, 11)
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    @ViewBuilder private func menu(_ row: HarnessRow, kind: MissionSectionKind) -> some View {
        if let target = MissionLayout.target(for: row, in: kind) {
            Button(openLabel(target), systemImage: openSymbol(target)) { open(row, kind: kind) }
        }
        Button(ChatCopy.copy, systemImage: "doc.on.doc") { UIPasteboard.general.string = row.title }
    }

    // MARK: Opening

    private func open(_ row: HarnessRow, kind: MissionSectionKind) {
        guard let target = MissionLayout.target(for: row, in: kind) else { return }
        switch target.kind {
        case .chat: push(.agent(target.ref))
        case .terminal: push(.job(row))
        case .screen: push(.screen(target.ref))
        case .approval: showChat(MissionLayout.needsItem(for: row, in: store.timeline)?.id)
        default: context.open(target)
        }
    }

    private func openLabel(_ target: HarnessOpen) -> String {
        switch target.kind {
        case .chat: MissionCopy.openChat
        case .terminal: MissionCopy.openTerminal
        case .diff: MissionCopy.openDiff
        case .url: MissionCopy.openPreview
        default: MissionCopy.more
        }
    }

    private func openSymbol(_ target: HarnessOpen) -> String {
        switch target.kind {
        case .chat: "bubble.left.and.text.bubble.right"
        case .terminal: "terminal"
        case .diff: "plusminus"
        case .url: "safari"
        default: "arrow.up.right"
        }
    }

    // MARK: Placeholder

    /// Reading or unavailable while there are no sections. It gives way to the sections in
    /// their `resize` transaction, and its own reading → unavailable swap crossfades in place.
    @ViewBuilder private func placeholder(empty: Bool) -> some View {
        ZStack {
            if empty {
                switch store.phase {
                case .describing, .opening, .reconnecting:
                    ProgressView(MissionCopy.reading)
                        .foregroundStyle(Color.togoMute)
                        // motion: state change — reading becomes unavailable in the same place.
                        .transition(Motion.swap(reduce: reduceMotion))
                case .live, .terminalOnly:
                    ContentUnavailableView {
                        Label(MissionCopy.unavailable, systemImage: "gauge.with.dots.needle.33percent")
                    } description: {
                        Text(MissionCopy.unavailableDetail)
                    }
                    .foregroundStyle(Color.togoMute)
                    // motion: state change — unavailable replaces reading in the same place.
                    .transition(Motion.swap(reduce: reduceMotion))
                }
            }
        }
        // motion: state change — the reading → unavailable crossfade, once the session answers.
        .motion(.respond, value: placeholderReading)
    }

    private var placeholderReading: Bool {
        switch store.phase {
        case .describing, .opening, .reconnecting: true
        case .live, .terminalOnly: false
        }
    }
}

/// Mission control in its own navigation stack, so agents, jobs and screens push inside the
/// sheet or the inspector and Back returns to the sections.
struct MissionControlStack: View {
    let store: HarnessSessionStore
    var actions: ChatActions = .inert
    /// Opens a diff or a preview in the review.
    var openExternal: (HarnessOpen) -> Void = { _ in }
    var showChat: (String?) -> Void = { _ in }
    /// Set in a sheet, which has a Done button; an inspector closes from its bar item.
    var onDone: (() -> Void)?

    @State private var path: [MissionRoute] = []

    var body: some View {
        NavigationStack(path: $path) {
            MissionControlView(
                store: store, context: context, actions: actions,
                push: { path.append($0) }, showChat: showChat
            )
            .toolbar { doneItem }
            .navigationDestination(for: MissionRoute.self) { route in
                destination(route)
            }
        }
        // Links in rows, cards and screens go through the chat's router, as in the chat.
        .environment(\.openURL, OpenURLAction { actions.open($0) })
    }

    @ToolbarContentBuilder private var doneItem: some ToolbarContent {
        if let onDone {
            ToolbarItem(placement: .confirmationAction) {
                Button(MissionCopy.done, systemImage: "checkmark", role: .confirm, action: onDone)
            }
        }
    }

    @ViewBuilder private func destination(_ route: MissionRoute) -> some View {
        switch route {
        case .agent(let id):
            SubagentChatView(store: store, agentID: id)
                .background(Color.togoInk)
        case .job(let row):
            // The live tail, and Open terminal when the process has a pane of its own; the host
            // attaches it as a tab (`AppStore.openHarnessTarget`).
            BackgroundTerminalView(store: store, itemID: row.id) { target in
                openExternal(HarnessOpen(kind: .terminal, ref: target.ref))
            }
        case .screen(let id):
            HarnessScreenLoader(
                store: store, screenID: id, openScreen: { path.append(.screen($0)) },
                open: { context.open($0) }, showTerminal: { context.showTerminal() }
            )
        }
    }

    private var context: PrimitiveContext {
        PrimitiveContext.store(
            store,
            openScreen: { path.append(.screen($0)) },
            open: { target in
                switch target.kind {
                case .screen: path.append(.screen(target.ref))
                case .chat: path.append(.agent(target.ref))
                default: openExternal(target)
                }
            },
            showTerminal: {
                onDone?()
                store.layer = .terminal
            }
        )
    }
}
