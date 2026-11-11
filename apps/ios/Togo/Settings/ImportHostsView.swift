import SwiftUI
import TogoKit
import UniformTypeIdentifiers

/// Paste SSH config text or choose a file, then tick the hosts to add as machines. Pushed from
/// the machines page, so Back leaves it and Import finishes it.
struct ImportHostsView: View {
    @Environment(\.dismiss) private var dismiss
    @Bindable var model: ImportHostsModel
    /// Opens the add-machine form filled in from one host.
    var openInForm: (ImportedHost) -> Void = { _ in }
    var finish: (Int) -> Void = { _ in }
    @State private var choosingFile = false

    var body: some View {
        List {
            if model.reviewed {
                preview
            } else {
                input
            }
        }
        .groupedList()
        // Paste → review, and the message, skipped and failure lines: rows arrive and leave on
        // `resize`.
        .resizesOnRows([
            "\(model.reviewed)", "\(model.message != nil)", "\(model.skipped.count)", "\(model.failure != nil)",
            "\(model.rows.count)",
        ])
        .navigationTitle(settingsText("import.hosts.screenTitle", "Import hosts"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if model.reviewed {
                ToolbarItem(placement: .confirmationAction) {
                    Button(importTitle, systemImage: "checkmark", role: .confirm) {
                        Task {
                            await model.importTicked()
                            if model.finished, let count = model.imported {
                                finish(count)
                                dismiss()
                            }
                        }
                    }
                    .disabled(model.tickedCount == 0 || model.saving)
                }
            }
        }
        .fileImporter(isPresented: $choosingFile, allowedContentTypes: [.plainText, .data]) { result in
            guard case .success(let url) = result else {
                model.reportUnreadable()
                return
            }
            model.load(url)
        }
    }

    private var importTitle: String {
        model.tickedCount == 0
            ? settingsText("import.hosts.import", "Import")
            : settingsText("import.hosts.importCount", "Import \(model.tickedCount)")
    }

    private var input: some View {
        Group {
            Section {
                TextEditor(text: $model.text)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoSnow)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: 180)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .accessibilityLabel(settingsText("import.hosts.field", "SSH config text"))
                Button {
                    choosingFile = true
                } label: {
                    Label(settingsText("import.hosts.chooseFile", "Choose file"), systemImage: "doc")
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect)
                }
            } header: {
                SectionHeading(settingsText("import.hosts.paste", "Paste your config"))
            } footer: {
                SectionFooter {
                    VStack(alignment: .leading, spacing: 8) {
                        if let message = model.message {
                            Label(message, systemImage: "exclamationmark.circle")
                                .foregroundStyle(Color.togoSnow)
                        }
                        Text(settingsText(
                            "import.hosts.keysNote",
                            "Keys aren't imported. New machines use this \(DeviceName.model)'s key; pick another in each machine's details."
                        ))
                    }
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                }
            }
            .groupedRows()
            if !model.skipped.isEmpty { skippedSection }
            Section {
                Button(settingsText("import.hosts.review", "Review hosts")) { model.review() }
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .disabled(model.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .groupedRows()
        }
    }

    private var skippedSection: some View {
        Section {
            ForEach(Array(model.skipped.enumerated()), id: \.offset) { _, line in
                VStack(alignment: .leading, spacing: 2) {
                    Text(line.line > 0 ? "\(line.keyword), line \(line.line)" : line.keyword)
                        .foregroundStyle(Color.togoSnow)
                    Text(line.reason)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                }
                .padding(.vertical, 4)
                .frame(minHeight: 44, alignment: .leading)
                .accessibilityElement(children: .combine)
            }
        } header: {
            SectionHeading(settingsText("import.hosts.skipped", "Skipped"))
        }
        .groupedRows()
    }

    @ViewBuilder private var preview: some View {
        Section {
            ForEach(model.rows) { row in
                ImportRowView(row: row, ticked: model.isTicked(row.id)) { model.toggle(row.id) }
                    .contextMenu {
                        Button(settingsText("import.hosts.openForm", "Add with the form"), systemImage: "square.and.pencil") {
                            openInForm(row.host)
                        }
                    }
            }
        } header: {
            SectionHeading(settingsText("import.hosts.hosts", "Hosts"))
        } footer: {
            SectionFooter {
                if let failure = model.failure {
                    Label(failure, systemImage: "exclamationmark.circle")
                        .font(.footnote)
                        .foregroundStyle(Color.togoSnow)
                }
            }
        }
        .groupedRows()
        if !model.skipped.isEmpty { skippedSection }
        Section {
            Button(settingsText("import.hosts.startOver", "Start over")) { model.startOver() }
                .frame(minHeight: 44)
        }
        .groupedRows()
    }
}

private struct ImportRowView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let row: ImportRow
    let ticked: Bool
    let toggle: () -> Void

    private var blocked: Bool {
        if case .blocked = row.status { true } else { false }
    }

    var body: some View {
        Button(action: toggle) {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: ticked ? "checkmark.circle.fill" : "circle")
                    .foregroundStyle(ticked ? Color.togoSnow : Color.togoMute)
                    .font(.title3)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(row.host.alias)
                        .foregroundStyle(blocked ? Color.togoMute : Color.togoSnow)
                    Text(row.address)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(Color.togoMute)
                    if let jump = row.jumpText {
                        Text(settingsText("import.hosts.via", "via \(jump)"))
                            .font(.subheadline.monospaced())
                            .foregroundStyle(Color.togoMute)
                    }
                    if let note {
                        Text(note)
                            .font(.subheadline)
                            .foregroundStyle(Color.togoMute)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.vertical, 4)
            .frame(minHeight: 44, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(ticked ? .isSelected : [])
        .accessibilityValue(blocked
            ? settingsText("import.hosts.unavailable", "Can't be imported")
            : ticked ? settingsText("import.hosts.selected", "Selected") : settingsText("import.hosts.notSelected", "Not selected"))
    }

    private var note: String? {
        switch row.status {
        case .ready: nil
        case .alreadyAdded: settingsText("import.hosts.already", "Already added")
        case .blocked(let reason): reason
        }
    }
}
