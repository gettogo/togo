import SwiftUI
import TogoKit

/// What the dictation page shows: the languages the engine offers, whether the chosen one's
/// speech model is on the device, and the recent dictations.
@MainActor @Observable final class DictationSettingsModel {
    private(set) var locales: [Locale]
    /// Nil while it is being checked.
    private(set) var availability: DictationAvailability?
    /// The fraction downloaded while a model installs.
    private(set) var installing: Double?
    private(set) var history: [String]
    let displayLocale: Locale
    @ObservationIgnored private let engine: any SpeechEngine
    @ObservationIgnored private let store: DictationHistory

    init(
        engine: any SpeechEngine = SpeechEngines.shared, history: DictationHistory = DictationHistory(),
        locales: [Locale] = [], availability: DictationAvailability? = nil, displayLocale: Locale = .current
    ) {
        self.engine = engine
        self.store = history
        self.history = history.load()
        self.availability = availability
        self.displayLocale = displayLocale
        self.locales = []
        self.locales = sorted(locales)
    }

    func load(_ locale: Locale) async {
        history = store.load()
        locales = sorted(await engine.supportedLocales)
        guard installing == nil else { return }
        availability = await engine.availability(locale: locale)
    }

    func install(_ locale: Locale) async {
        guard installing == nil else { return }
        installing = 0
        do {
            try await engine.installModel(locale: locale) { [weak self] in self?.installing = $0 }
            installing = nil
            availability = await engine.availability(locale: locale)
        } catch {
            installing = nil
            availability = .unavailable(error.localizedDescription)
        }
    }

    func clear() {
        store.clear()
        history = []
    }

    func name(_ locale: Locale) -> String {
        DictationCopy.language(for: locale, in: displayLocale)
    }

    private func sorted(_ locales: [Locale]) -> [Locale] {
        locales.sorted { name($0).localizedStandardCompare(name($1)) == .orderedAscending }
    }
}

/// Settings > Dictation: the language, its speech model, and recent dictations to use again.
struct DictationSettingsView: View {
    @FocusedValue(\.appStore) private var store
    @Environment(\.openURL) private var openURL
    @State private var model: DictationSettingsModel
    @State private var engine: WhisperSettingsModel
    let settings: AppSettings
    let update: (AppSettings) -> Void
    private let insertOverride: ((String) -> Void)?

    /// `insert` stands in for the window's store, for previews and snapshots.
    init(
        settings: AppSettings, update: @escaping (AppSettings) -> Void,
        model: DictationSettingsModel = DictationSettingsModel(), engine: WhisperSettingsModel = WhisperSettingsModel(),
        insert: ((String) -> Void)? = nil
    ) {
        self.settings = settings
        self.update = update
        _model = State(initialValue: model)
        _engine = State(initialValue: engine)
        insertOverride = insert
    }

    private var locale: Locale {
        SpeechEngines.locale(settings.dictationLocale)
    }

    var body: some View {
        List {
            DictationEngineSection(model: engine, update: update)
            Section {
                languagePicker
            } footer: {
                Text(DictationCopy.languageFooter)
            }
            .groupedRows()
            Section {
                speechRow
            } footer: {
                Text(DictationCopy.speechFooter)
            }
            .groupedRows()
            Section {
                recent
            } header: {
                Text(DictationCopy.recent)
            } footer: {
                Text(DictationCopy.recentFooter)
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(DictationCopy.title)
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: settings, initial: true) {
            engine.settings = settings
            SpeechEngines.select(settings)
        }
        .task(id: [settings.dictationLocale, settings.dictationEngine.rawValue, settings.whisperModel]) {
            // Selected here too, so the languages and model shown are the chosen engine's.
            SpeechEngines.select(settings)
            await model.load(locale)
        }
    }

    // MARK: Language

    private var languagePicker: some View {
        Picker(DictationCopy.language, selection: languageBinding) {
            Text(DictationCopy.automatic).tag(String?.none)
            ForEach(choices, id: \.identifier) { locale in
                Text(model.name(locale)).tag(String?.some(locale.identifier))
            }
        }
        .frame(minHeight: 44)
    }

    /// The offered languages, plus the stored one if the engine no longer lists it.
    private var choices: [Locale] {
        guard let stored = settings.dictationLocale,
              !model.locales.contains(where: { $0.identifier == stored }) else { return model.locales }
        return [Locale(identifier: stored)] + model.locales
    }

    private var languageBinding: Binding<String?> {
        Binding(
            get: { settings.dictationLocale },
            set: { identifier in
                var changed = settings
                changed.dictationLocale = identifier
                update(changed)
            }
        )
    }

    // MARK: Speech model

    @ViewBuilder private var speechRow: some View {
        let language = model.name(locale)
        if let fraction = model.installing {
            VStack(alignment: .leading, spacing: 8) {
                Text(DictationCopy.downloading(language: language))
                ProgressView(value: fraction)
                    .tint(Color.togoSnow)
            }
            .padding(.vertical, 6)
            .accessibilityElement(children: .combine)
        } else {
            switch model.availability {
            case nil:
                status(DictationCopy.speechFor(language), value: DictationCopy.checking)
            case .ready?:
                status(DictationCopy.speechFor(language), value: DictationCopy.installed)
            case .needsModel(let bytes)?:
                Button(DictationCopy.needsModel(language: language, bytes: bytes)) {
                    Task { await model.install(locale) }
                }
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
            case .unsupportedLocale?:
                Text(DictationCopy.unsupported)
                    .frame(minHeight: 44)
            case .microphoneDenied?:
                VStack(alignment: .leading, spacing: 4) {
                    Text(DictationCopy.denied)
                    if let url = URL(string: UIApplication.openSettingsURLString) {
                        Button(DictationCopy.openSettings) { openURL(url) }
                            .fontWeight(.semibold)
                            .foregroundStyle(Color.togoSnow)
                            .frame(minHeight: 44)
                    }
                }
                .padding(.top, 6)
            case .unavailable(let reason)?:
                Text(reason)
                    .frame(minHeight: 44)
            }
        }
    }

    private func status(_ title: String, value: String) -> some View {
        LabeledContent {
            Text(value).foregroundStyle(Color.togoMute)
        } label: {
            Text(title)
        }
        .frame(minHeight: 44)
    }

    // MARK: Recent

    @ViewBuilder private var recent: some View {
        if model.history.isEmpty {
            Text(DictationCopy.noneYet)
                .foregroundStyle(Color.togoMute)
                .frame(minHeight: 44)
        } else {
            ForEach(model.history, id: \.self) { entry in
                RecentDictationRow(text: entry, insert: insertAction.map { insert in { insert(entry) } })
            }
            Button(DictationCopy.clear, role: .destructive, action: model.clear)
                .frame(minHeight: 44)
        }
    }

    private var insertAction: ((String) -> Void)? {
        if let insertOverride { return insertOverride }
        guard let store, store.composeTab != nil else { return nil }
        return { store.insertDictation($0) }
    }
}

/// One recent dictation. At accessibility sizes Insert moves under the words.
private struct RecentDictationRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let text: String
    let insert: (() -> Void)?

    var body: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            Text(text)
                .lineLimit(3)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let insert {
                Button(DictationCopy.insert, action: insert)
                    .buttonStyle(.borderless)
                    .fontWeight(.semibold)
                    .foregroundStyle(Color.togoSnow)
                    .frame(minHeight: 44)
                    .fixedSize()
            }
        }
        .padding(.vertical, 4)
    }
}
