import Foundation
import TogoKit

/// The Settings primitive's rules, apart from the view: what a commit sends, how numbers clamp
/// and read, and what a secret field may show (only whether it is set, never a value).
enum SettingsLogic {
    typealias Field = HarnessScreen.Settings.Field

    /// Committing a field runs its action with exactly `{"value": v}`.
    static func commitArgs(_ value: JSONValue) -> [String: JSONValue] {
        ["value": value]
    }

    /// A field the user can change: it has an action and is not read only.
    static func isEditable(_ field: Field) -> Bool {
        field.action?.isEmpty == false && field.readOnly != true
    }

    static func bool(_ value: JSONValue) -> Bool {
        switch value {
        case .bool(let on): on
        case .number(let n): n != 0
        case .string(let s): PickerModel.isTrue(s)
        default: false
        }
    }

    static func string(_ value: JSONValue) -> String? {
        switch value {
        case .string(let s): s
        case .number(let n): numberText(n)
        case .bool(let b): b ? "true" : "false"
        default: nil
        }
    }

    static func number(_ value: JSONValue) -> Double? {
        switch value {
        case .number(let n): n
        case .string(let s): Double(s.trimmingCharacters(in: .whitespaces))
        default: nil
        }
    }

    /// Whole numbers without a decimal point; others to at most three places.
    static func numberText(_ value: Double) -> String {
        if value.rounded() == value, abs(value) < 1e15 { return String(Int64(value)) }
        return value.formatted(.number.precision(.fractionLength(0...3)).grouping(.never))
    }

    static func clamp(_ value: Double, _ field: Field) -> Double {
        var v = value
        if let min = field.min { v = Swift.max(min, v) }
        if let max = field.max { v = Swift.min(max, v) }
        return v
    }

    /// One for whole-number ranges, a tenth for small fractional ones.
    static func step(_ field: Field) -> Double {
        guard let min = field.min, let max = field.max else { return 1 }
        let whole = min.rounded() == min && max.rounded() == max
        return whole || max - min > 10 ? 1 : 0.1
    }

    /// The typed text as the field's value: a clamped number for number fields, text as is.
    /// nil when a number field's text is not a number.
    static func parse(_ text: String, for field: Field) -> JSONValue? {
        switch field.kind {
        case .number:
            guard let n = Double(text.trimmingCharacters(in: .whitespaces)) else { return nil }
            return .number(clamp(n, field))
        default:
            return .string(text)
        }
    }

    /// What the field starts with when the user edits it. A secret starts empty, always.
    static func initialText(_ field: Field) -> String {
        guard field.kind != .secret else { return "" }
        return string(field.value) ?? ""
    }

    /// "Set" or "Not set" for a secret, from `isSet` alone.
    static func secretStatus(_ field: Field) -> String {
        field.isSet == true ? PrimitiveCopy.set : PrimitiveCopy.notSet
    }

    /// The chosen option's label, or the raw value when the adapter sent one it does not list.
    static func choiceLabel(_ field: Field, value: JSONValue) -> String? {
        guard let id = string(value) else { return nil }
        return field.options?.first { $0.id == id }?.label ?? id
    }

    /// Whether a choice reads best as a list of options with their details, rather than a menu.
    static func showsInline(_ field: Field) -> Bool {
        let options = field.options ?? []
        return options.count <= 6 && options.contains { $0.detail?.isEmpty == false }
    }

    /// A choice that is not inline pushes a list (every value wraps in full) when it has more
    /// than a menu's handful of options or any long label; otherwise it is a menu.
    static func choosesInList(_ field: Field) -> Bool {
        let options = field.options ?? []
        return options.count > 7 || options.contains { $0.label.count > 24 }
    }

    /// A form with one field: a change that lands closes it.
    static func isSingleField(_ settings: HarnessScreen.Settings) -> Bool {
        settings.sections.reduce(0) { $0 + $1.fields.count } == 1
    }

    /// The change a commit makes, for `ActionGate`: the field's confirm, its new value in words
    /// (a secret never named), and its current value to go back to.
    static func change(_ field: Field, to value: JSONValue, action: String) -> GatedChange {
        GatedChange(
            action: action, target: field.id, args: commitArgs(value), confirm: field.actionConfirm,
            setting: field.label, value: words(field, value),
            previous: field.kind == .secret ? nil : someWords(field, field.value).map { ($0, commitArgs(field.value)) }
        )
    }

    /// A value as the chat says it: an option's label, on or off, a number, the text; a
    /// secret is never named.
    static func words(_ field: Field, _ value: JSONValue) -> String {
        switch field.kind {
        case .secret: return PrimitiveCopy.set
        case .toggle: return bool(value) ? PrimitiveCopy.on : PrimitiveCopy.off
        case .choice: return choiceLabel(field, value: value) ?? ""
        case .number: return number(value).map(numberText) ?? ""
        default: return string(value) ?? ""
        }
    }

    private static func someWords(_ field: Field, _ value: JSONValue) -> String? {
        let text = words(field, value)
        return text.isEmpty ? nil : text
    }
}
