import SwiftUI

/// The sheet that starts a session: machine, folder, then agent. It opens as a zoom from the
/// plus button when `zoom` names that button.
struct NewSessionSheet: View {
    struct Zoom {
        var sourceID: AnyHashable
        var namespace: Namespace.ID
    }

    @Environment(\.dismiss) private var dismiss
    @State private var model: NewSessionModel
    @State private var path: [NewSessionModel.Step]
    @State private var detent = SheetStyle.formStart
    private let zoom: Zoom?

    init(model: NewSessionModel, zoom: Zoom? = nil) {
        _model = State(initialValue: model)
        _path = State(initialValue: model.pushedSteps)
        self.zoom = zoom
    }

    var body: some View {
        zoomed {
            NavigationStack(path: $path) {
                screen(for: model.firstStep)
                    .navigationDestination(for: NewSessionModel.Step.self) { screen(for: $0) }
            }
        }
        .togoSheet(.form, detent: $detent)
        .tint(.primary)
        .toggleStyle(.systemSwitch)
        .task { await model.start() }
        .onChange(of: path) {
            // Going back from the browser leaves it; choosing in it has left it already.
            if !path.contains(.browse), model.browsing != .idle { model.leaveBrowsing() }
            model.step = path.last { $0 != .browse } ?? model.firstStep
            // A folder tree wants the full height; the system moves the sheet between heights.
            if path.contains(.browse) { detent = .large }
        }
        .haptic(.error, trigger: model.createError, when: { $0 != nil })
    }

    /// Every step can close the sheet, not only the first; Back stays the system's.
    private func screen(for step: NewSessionModel.Step) -> some View {
        page(for: step)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(role: .close) { dismiss() }
                }
            }
    }

    @ViewBuilder private func page(for step: NewSessionModel.Step) -> some View {
        switch step {
        case .machine:
            MachineStep(model: model) { machine in
                guard path.last != .folder else { return }
                path.append(.folder)
                await model.choose(machine)
            }
        case .folder:
            FolderStep(model: model, onStart: start, onBrowse: browse, onChoose: choose(folder:))
        case .browse:
            BrowseStep(model: model, onChoose: choose(folder:))
        case .agent:
            AgentStep(model: model, onStart: start)
        }
    }

    @ViewBuilder private func zoomed(@ViewBuilder _ content: () -> some View) -> some View {
        if let zoom {
            content().navigationTransition(.zoom(sourceID: zoom.sourceID, in: zoom.namespace))
        } else {
            content()
        }
    }

    /// The browser is a value in the path like every other page, so choosing a folder in it
    /// changes the path once. A page presented by a flag on the root step, popped while the
    /// agent step is pushed, leaves the stack on an empty page with only a back button.
    private func browse() async {
        guard path.last != .browse else { return }
        path.append(.browse)
        await model.browse("~")
    }

    /// One change to the path: the browser, if open, is replaced by the agent step.
    private func choose(folder: String) async {
        guard path.last != .agent else { return }
        path = path.filter { $0 != .browse } + [.agent]
        await model.choose(folder: folder)
    }

    private func start() async {
        if await model.create() != nil { dismiss() }
    }
}

/// Scroll container shared by the three steps: a title, a lead line and the content.
struct StepScaffold<Content: View>: View {
    let title: String
    var prompt: String?
    @ViewBuilder let content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if let prompt {
                    Text(prompt)
                        .font(.title3.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityAddTraits(.isHeader)
                }
                content
            }
            .screenMargins()
            .padding(.vertical, 12)
        }
        // No fill of its own: the sheet's glass shows through.
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .scrollBounceBehavior(.basedOnSize)
    }
}

/// A tappable line with an icon, a title and optional detail, separated from the next by an
/// inset hairline. Text wraps freely at accessibility sizes.
struct ChoiceRow<Detail: View>: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var iconWidth: CGFloat = 30
    let symbol: String
    let title: Text
    var selected = false
    var chevron = false
    var enabled = true
    let action: () -> Void
    @ViewBuilder let detail: Detail

    /// An unavailable row is plain text rather than a disabled button: the system dims disabled
    /// buttons below 4.5:1, and the reason it gives must stay readable.
    var body: some View {
        if enabled {
            Button(action: action) { content }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
        } else {
            content
                .accessibilityElement(children: .combine)
        }
    }

    private var content: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Image(systemName: symbol)
                .font(dynamicTypeSize.isAccessibilitySize ? .body : .title3)
                .frame(width: iconWidth, alignment: .leading)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                title
                    .font(.body)
                    .fixedSize(horizontal: false, vertical: true)
                detail
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
            Spacer(minLength: 8)
            if selected {
                Image(systemName: "checkmark").fontWeight(.semibold).accessibilityHidden(true)
            } else if chevron {
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
            }
        }
        .foregroundStyle(enabled ? Color.togoSnow : Color.togoMute)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(Rectangle())
        .overlay(alignment: .bottom) { Color.togoLine.frame(height: 1) }
    }
}

extension ChoiceRow where Detail == EmptyView {
    init(
        symbol: String, title: Text, selected: Bool = false, chevron: Bool = false, enabled: Bool = true,
        action: @escaping () -> Void
    ) {
        self.init(symbol: symbol, title: title, selected: selected, chevron: chevron, enabled: enabled, action: action) {
            EmptyView()
        }
    }
}

/// A quiet text button for retrying a failed load.
struct RetryButton: View {
    let action: () -> Void

    var body: some View {
        Button(NewSessionCopy.retry, action: action)
            .buttonStyle(.secondary(.fit))
    }
}

/// Plain error line shown on the step the failure belongs to.
struct InlineError: View {
    let message: String

    var body: some View {
        Label(message, systemImage: "exclamationmark.circle")
            .font(.subheadline)
            .foregroundStyle(Color.togoSnow)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityLabel(message)
    }
}

#if DEBUG
#Preview("Machine") {
    NewSessionSheet(model: NewSessionModel(services: FakeAppServices(), hostID: nil, machines: Samples.controlCenter.machines))
}

#Preview("Folder") {
    NewSessionSheet(model: NewSessionModel(services: FakeAppServices(), hostID: Samples.studio, machines: Samples.controlCenter.machines))
}
#endif
