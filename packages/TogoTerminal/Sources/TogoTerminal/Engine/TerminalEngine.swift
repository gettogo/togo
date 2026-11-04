import Foundation

/// The terminal surface the app talks to. Every SwiftTerm call stays behind it.
///
/// There is no `resize`: on iOS the emulator derives its grid from the view frame, so the
/// engine reports size through `onResize` and the transport follows it.
@MainActor public protocol TerminalEngine: AnyObject {
    func feed(_ bytes: ArraySlice<UInt8>)
    var onInput: (Data) -> Void { get set }
    var onResize: ((cols: Int, rows: Int)) -> Void { get set }
    var size: (cols: Int, rows: Int) { get }
    func send(_ key: KeyStroke)
    func paste(_ text: String)
}
