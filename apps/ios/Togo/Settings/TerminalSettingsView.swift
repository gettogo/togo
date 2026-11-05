import SwiftUI
import TogoKit
import TogoTerminal
import UniformTypeIdentifiers

struct TerminalSettingsView: View {
    let settings: AppSettings
    let fonts: [String]
    let update: (AppSettings) -> Void
    @State private var importing = false
    @State private var importError: String?

    init(settings: AppSettings, fonts: [String], update: @escaping (AppSettings) -> Void, importError: String? = nil) {
        self.settings = settings
        self.fonts = fonts
        self.update = update
        _importError = State(initialValue: importError)
    }

    private var choices: [ThemeChoice] { ThemeChoice.all(imported: settings.customThemes) }
    private var selected: ThemeChoice { choices.first { $0.id == settings.themeID } ?? choices[0] }

    private static let importRow = "import"

    var body: some View {
        ScrollViewReader { proxy in
            list
                // The error sits under the import button; at large text sizes that is below the fold.
                .onAppear { revealImportError(proxy) }
                .onChange(of: importError) { revealImportError(proxy) }
        }
        .navigationTitle(settingsText("settings.terminal.title", "Terminal"))
        .navigationBarTitleDisplayMode(.inline)
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item]) { result in
            receive(result)
        }
    }

    private func revealImportError(_ proxy: ScrollViewProxy) {
        if importError != nil {
            proxy.scrollTo(Self.importRow, anchor: .center)
        }
    }

    private var list: some View {
        List {
            Section {
                ThemePreview(choice: selected, settings: settings)
                    .clipShape(.rect(cornerRadius: 10))
                    // A theme background close to the row color would otherwise lose the preview's edge.
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color.togoLine))
                    .listRowInsets(EdgeInsets(top: 12, leading: 12, bottom: 12, trailing: 12))
            }
            .groupedRows()
            Section {
                ForEach(choices) { choice in
                    themeRow(choice)
                }
                Button {
                    importing = true
                } label: {
                    Label(settingsText("settings.terminal.import", "Import theme"), systemImage: "square.and.arrow.down")
                }
                .frame(minHeight: 44)
                .id(Self.importRow)
            } footer: {
                importFooter
            }
            .groupedRows()
            Section {
                Picker(settingsText("settings.terminal.font", "Font"), selection: settings.binding(\.fontName, update)) {
                    ForEach(fontOptions, id: \.self) { Text($0).tag($0) }
                }
                Stepper(value: settings.binding(\.fontSize, update), in: 9...24, step: 1) {
                    LabeledContent(
                        settingsText("settings.terminal.size", "Size"),
                        value: settingsText("settings.terminal.points", "\(Int(settings.fontSize)) pt")
                    )
                }
                Picker(settingsText("settings.terminal.cursor", "Cursor"), selection: settings.binding(\.cursor, update)) {
                    ForEach(CursorShape.choices, id: \.rawValue) { shape in
                        Text(shape.title).tag(shape.rawValue)
                    }
                }
            }
            .groupedRows()
        }
        .groupedList()
    }

    private var fontOptions: [String] {
        fonts.contains(settings.fontName) ? fonts : fonts + [settings.fontName]
    }

    @ViewBuilder private var importFooter: some View {
        if let importError {
            Label(importError, systemImage: "exclamationmark.circle")
                .foregroundStyle(Color.togoSnow)
        } else {
            Text(settingsText("settings.terminal.import.footer", "Reads iTerm2, Ghostty and Xresources theme files."))
        }
    }

    private func themeRow(_ choice: ThemeChoice) -> some View {
        Button {
            var changed = settings
            changed.themeID = choice.id
            update(changed)
        } label: {
            HStack(spacing: 14) {
                ThemeSwatch(choice: choice)
                VStack(alignment: .leading, spacing: 2) {
                    Text(choice.name)
                        .foregroundStyle(Color.togoSnow)
                    if let detail = choice.detail {
                        Text(detail)
                            .font(.subheadline)
                            .foregroundStyle(Color.togoMute)
                    }
                }
                Spacer(minLength: 8)
                if choice.id == settings.themeID {
                    Image(systemName: "checkmark")
                        .fontWeight(.semibold)
                        .foregroundStyle(Color.togoSnow)
                }
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .accessibilityAddTraits(choice.id == settings.themeID ? .isSelected : [])
        .swipeActions {
            if choice.isImported {
                Button(settingsText("settings.terminal.removeTheme", "Remove"), role: .destructive) {
                    removeImported(choice.id)
                }
            }
        }
    }

    private func receive(_ result: Result<URL, any Error>) {
        do {
            let theme = try ThemeImport.read(try result.get())
            var changed = settings
            changed.customThemes.removeAll { $0.id == theme.id }
            changed.customThemes.append(theme)
            changed.themeID = theme.id
            importError = nil
            update(changed)
        } catch {
            importError = ThemeImport.message(for: error, filename: (try? result.get())?.lastPathComponent)
        }
    }

    private func removeImported(_ id: String) {
        var changed = settings
        changed.customThemes.removeAll { $0.id == id }
        if changed.themeID == id {
            changed.themeID = ThemeChoice.beacon
        }
        update(changed)
    }
}

/// A theme as the list offers it. Beacon follows the system appearance; every other theme
/// looks the same in light and dark.
struct ThemeChoice: Identifiable, Equatable {
    static let beacon = "beacon"

    var id: String
    var name: String
    var detail: String?
    var dark: TerminalTheme
    var light: TerminalTheme
    var isImported = false

    func theme(for scheme: ColorScheme) -> TerminalTheme {
        scheme == .dark ? dark : light
    }

    static func all(imported: [TerminalTheme]) -> [ThemeChoice] {
        let builtIn = BuiltInThemes.all
        var choices: [ThemeChoice] = []
        if let dark = builtIn.first(where: { $0.id == "beacon-dark" }),
           let light = builtIn.first(where: { $0.id == "beacon-light" }) {
            choices.append(ThemeChoice(
                id: beacon, name: "Beacon",
                detail: settingsText("settings.terminal.beacon.detail", "Light or dark with the system"),
                dark: dark, light: light
            ))
        }
        choices += builtIn.map { ThemeChoice(id: $0.id, name: $0.name, dark: $0, light: $0) }
        choices += imported.map { ThemeChoice(id: $0.id, name: $0.name, dark: $0, light: $0, isImported: true) }
        return choices
    }
}

/// Reading a theme file the user picked. Files come from anywhere, so size is capped before
/// parsing and every failure becomes one sentence.
enum ThemeImport {
    static let maximumBytes = 512 * 1024

    struct TooLarge: Error {}

    static func read(_ url: URL) throws -> TerminalTheme {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        return try ThemeImporter.load(try capped(handle), filename: url.lastPathComponent)
    }

    /// Reads one byte past the cap so a file whose size the provider does not report, or that
    /// grows while it is read, is still refused.
    static func capped(_ handle: FileHandle) throws -> Data {
        let data = try handle.read(upToCount: maximumBytes + 1) ?? Data()
        guard data.count <= maximumBytes else { throw TooLarge() }
        return data
    }

    static func message(for error: any Error, filename: String?) -> String {
        guard let filename, !filename.isEmpty else {
            return settingsText("settings.terminal.import.unopened", "Couldn't open that file. Choose an iTerm2, Ghostty or Xresources theme.")
        }
        switch error {
        case ThemeImportError.missingColor(let color) where color.hasPrefix("ansi "):
            let index = String(color.dropFirst("ansi ".count))
            return settingsText("settings.terminal.import.missingANSI", "Couldn't import \(filename): it has no ANSI color \(index).")
        case ThemeImportError.missingColor(let color):
            return settingsText("settings.terminal.import.missing", "Couldn't import \(filename): it has no \(color) color.")
        case ThemeImportError.invalidColor(let key):
            return settingsText("settings.terminal.import.invalid", "Couldn't import \(filename): the \(key) color isn't a hex color.")
        case is TooLarge:
            return settingsText("settings.terminal.import.large", "Couldn't import \(filename): it's too large to be a theme.")
        default:
            return settingsText("settings.terminal.import.unreadable", "Couldn't read \(filename). Choose an iTerm2, Ghostty or Xresources theme.")
        }
    }
}

extension CursorShape {
    static let choices: [CursorShape] = [.block, .underline, .bar]

    var title: String {
        switch self {
        case .block: settingsText("settings.terminal.cursor.block", "Block")
        case .underline: settingsText("settings.terminal.cursor.underline", "Underline")
        case .bar: settingsText("settings.terminal.cursor.bar", "Bar")
        }
    }
}

extension Color {
    /// Theme colors are validated `#RRGGBB` strings by the time they reach a view.
    init(terminalHex hex: String) {
        let value = UInt32(hex.trimmingCharacters(in: CharacterSet(charactersIn: "#")), radix: 16) ?? 0
        self.init(
            red: Double((value >> 16) & 0xFF) / 255,
            green: Double((value >> 8) & 0xFF) / 255,
            blue: Double(value & 0xFF) / 255
        )
    }
}

/// A few lines of a shell drawn in the theme's own colors, font, size and cursor.
private struct ThemePreview: View {
    @Environment(\.colorScheme) private var colorScheme
    let choice: ThemeChoice
    let settings: AppSettings

    var body: some View {
        let theme = choice.theme(for: colorScheme)
        let size = CGFloat(settings.fontSize)
        VStack(alignment: .leading, spacing: size * 0.25) {
            Text(line(theme, [("sam@studio ", 2), ("~/src/app", 4)]))
            Text(line(theme, [("$ git diff --stat", nil)]))
            Text(line(theme, [(" routes.ts | 9 ", nil), ("+++++", 2), ("----", 1)]))
            HStack(spacing: 0) {
                Text(line(theme, [("$ ", nil)]))
                cursor(theme, size: size)
            }
        }
        .font(terminalFont(size))
        .lineLimit(1)
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(terminalHex: theme.background))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(settingsText("settings.terminal.preview", "Preview of \(choice.name)"))
    }

    private func terminalFont(_ size: CGFloat) -> Font {
        settings.fontName == "SF Mono" ? .system(size: size, design: .monospaced) : .custom(settings.fontName, fixedSize: size)
    }

    private func line(_ theme: TerminalTheme, _ runs: [(String, Int?)]) -> AttributedString {
        runs.reduce(into: AttributedString()) { text, run in
            var piece = AttributedString(run.0)
            piece.foregroundColor = Color(terminalHex: run.1.map { theme.ansi[$0] } ?? theme.foreground)
            text += piece
        }
    }

    @ViewBuilder private func cursor(_ theme: TerminalTheme, size: CGFloat) -> some View {
        let color = Color(terminalHex: theme.cursor)
        let cell = CGSize(width: size * 0.6, height: size * 1.2)
        switch CursorShape(rawValue: settings.cursor) ?? .block {
        case .block: color.frame(width: cell.width, height: cell.height)
        case .underline: color.frame(width: cell.width, height: 2).frame(height: cell.height, alignment: .bottom)
        case .bar: color.frame(width: 2, height: cell.height)
        }
    }
}

/// The small tile on each theme row: the theme's background with a prompt in its colors.
private struct ThemeSwatch: View {
    @Environment(\.colorScheme) private var colorScheme
    let choice: ThemeChoice

    var body: some View {
        let theme = choice.theme(for: colorScheme)
        HStack(spacing: 3) {
            ForEach([1, 2, 3, 4], id: \.self) { index in
                Color(terminalHex: theme.ansi[index])
                    .frame(width: 6, height: 14)
            }
        }
        .frame(width: 52, height: 34)
        .background(Color(terminalHex: theme.background), in: .rect(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(Color.togoLine))
        .accessibilityHidden(true)
    }
}
