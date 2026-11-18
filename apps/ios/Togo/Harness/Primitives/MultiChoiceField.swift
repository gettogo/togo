import SwiftUI
import TogoKit

/// Multiple values use the same native settings navigation as a long choice.
struct MultiChoiceField: View {
    let field: HarnessScreen.Settings.Field
    let value: JSONValue
    let editable: Bool
    let commit: (JSONValue) -> Void

    private var selected: Set<String> {
        guard case .array(let values) = value else { return [] }
        return Set(values.compactMap(SettingsLogic.string))
    }

    var body: some View {
        NavigationLink {
            Form {
                ForEach(field.options ?? []) { option in
                    Toggle(isOn: Binding(get: { selected.contains(option.id) }, set: { on in
                        var next = selected
                        if on { next.insert(option.id) } else { next.remove(option.id) }
                        let ordered = (field.options ?? []).map(\.id).filter(next.contains)
                        commit(.array(ordered.map(JSONValue.string)))
                    })) {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(verbatim: option.shownTitle)
                            if let detail = option.shownDetail {
                                Text(verbatim: detail).font(.footnote).foregroundStyle(Color.togoMute)
                            }
                        }
                    }
                    .disabled(!editable || option.disabled == true)
                    .listRowBackground(Color.togoGrouped)
                }
            }
            .scrollContentBackground(.hidden)
            .background(Color.togoInk)
            .navigationTitle(field.label)
        } label: {
            LabeledContent(field.label) {
                Text(verbatim: SettingsLogic.words(field, value)).foregroundStyle(Color.togoMute)
            }
        }
        .disabled(!editable)
    }
}
