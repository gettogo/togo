import SwiftUI
import TogoTerminal
import UIKit

/// One terminal's keys as the app runs them: installs the overlay on the terminal's view once
/// it loads, plays its haptics, presents its palette, and hands prompt answers and the edit
/// request to the store.
@MainActor final class TerminalKeysHost {
    /// What the host knows, buffered until the overlay exists.
    var input = TerminalKeysInput() {
        didSet { overlay?.input = input }
    }
    var onPrompt: (PromptKey) -> Void = { _ in }
    var onAttach: () -> Void = {}
    var onDictate: () -> Void = {}
    /// "Edit keys": the host opens Settings.
    var onEdit: () -> Void = {}
    private(set) var overlay: TerminalKeysOverlay?
    private weak var engine: SwiftTermEngine?
    private weak var terminal: TerminalViewController?
    private weak var presenter: UIViewController?
    private weak var palette: UIViewController?

    init(engine: SwiftTermEngine, terminal: TerminalViewController) {
        self.engine = engine
        self.terminal = terminal
    }

    /// Installs the overlay on `controller`'s view. Called from `viewDidLoad`.
    func install(on controller: UIViewController) {
        guard overlay == nil, let engine, let terminal else { return }
        presenter = controller
        let overlay = TerminalKeysOverlay(engine: engine, terminal: terminal, host: controller.view)
        overlay.onHaptic = { Haptic($0).play() }
        overlay.onPrompt = { [weak self] in self?.onPrompt($0) }
        overlay.onAttach = { [weak self] in self?.onAttach() }
        overlay.onDictate = { [weak self] in self?.onDictate() }
        overlay.onPalette = { [weak self] anchor, edit in self?.showPalette(from: anchor, edit: edit) }
        overlay.input = input
        overlay.column.controller.accessibilityLabel = KeysCopy.terminalKeys
        overlay.column.controller.accessibilityHint = KeysCopy.controllerHint
        self.overlay = overlay
    }

    /// Looks again after the host moved the terminal: a fold, a resize, a new window.
    func refresh() {
        overlay?.refresh()
    }

    /// Wires the bar's capsule to this terminal while it is the focused one.
    func attachDockFace(_ face: ControllerFace) {
        overlay?.attach(face)
        face.accessibilityLabel = KeysCopy.terminalKeys
        face.accessibilityHint = KeysCopy.controllerHint
    }

    // MARK: Palette

    /// The palette as a popover from `anchor`, on every size class: the keys belong beside the
    /// controller, not in a sheet. A hold opens Settings instead, where the keys are edited.
    func showPalette(from anchor: UIView, edit: Bool) {
        if let palette {
            palette.dismiss(animated: true)
            self.palette = nil
        }
        if edit {
            onEdit()
            return
        }
        guard let overlay, let presenter else { return }
        let view = KeysPaletteView(
            config: input.config, agent: input.agent, hidesInputTools: input.hidesInputTools,
            latch: overlay.latch, hasMode: AgentKeys.keys(for: input.agent).mode != nil,
            press: { [weak self] ref in self?.overlay?.press(ref); self?.closeAfter(ref) },
            lock: { [weak self] modifier, on in self?.overlay?.lock(modifier, on) },
            perform: { [weak self] item in self?.overlay?.perform(item); self?.dismissPalette() },
            edit: { [weak self] in self?.dismissPalette(); self?.onEdit() }
        )
        let controller = UIHostingController(rootView: view)
        controller.modalPresentationStyle = .popover
        controller.preferredContentSize = CGSize(width: KeysPaletteView.width, height: KeysPaletteView.height)
        controller.sizingOptions = .preferredContentSize
        if let popover = controller.popoverPresentationController {
            popover.sourceView = anchor
            popover.sourceRect = anchor.bounds
            popover.permittedArrowDirections = [.down, .right, .left, .up]
            popover.delegate = PopoverStaysPopover.shared
        }
        presenter.present(controller, animated: !UIAccessibility.isReduceMotionEnabled)
        palette = controller
    }

    private func closeAfter(_ ref: KeyRef) {
        // Latches stay open so a second tap can lock; every other key is one and done.
        if ref.latch == nil { dismissPalette() }
    }

    func dismissPalette() {
        palette?.dismiss(animated: !UIAccessibility.isReduceMotionEnabled)
        palette = nil
    }
}

/// Keeps a popover a popover in a compact window, where UIKit would turn it into a sheet.
@MainActor final class PopoverStaysPopover: NSObject, UIPopoverPresentationControllerDelegate {
    static let shared = PopoverStaysPopover()

    func adaptivePresentationStyle(
        for controller: UIPresentationController, traitCollection: UITraitCollection
    ) -> UIModalPresentationStyle {
        .none
    }
}
