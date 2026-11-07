import Foundation

/// An action a screen, row or status block offers; run with `harness.action`.
public struct HarnessActionRef: Codable, Hashable, Sendable, Identifiable {
    public struct Role: OpenString {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public static let normal: Self = "normal"
        public static let primary: Self = "primary"
        public static let destructive: Self = "destructive"
    }

    public var id: String
    public var label: String
    public var role: Role
    /// Asked before the action runs, when set.
    public var confirm: String?
    /// Fixed args merged under the user's.
    public var args: [String: JSONValue]?

    public init(id: String, label: String, role: Role = .normal, confirm: String? = nil, args: [String: JSONValue]? = nil) {
        self.id = id
        self.label = label
        self.role = role
        self.confirm = confirm
        self.args = args
    }
}

/// Asked before a selection or a field change runs. `effect` relaunch: the change restarts the agent,
/// so the app confirms only while it works and offers Undo when it is idle.
public struct HarnessSelectConfirm: Codable, Hashable, Sendable {
    public struct Effect: OpenString {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public static let relaunch: Self = "relaunch"
    }

    public var text: String
    public var effect: Effect?

    public init(text: String, effect: Effect? = nil) {
        self.text = text
        self.effect = effect
    }
}

/// The seven primitives every native screen is drawn with.
public struct HarnessPrimitive: OpenString {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
    public static let picker: Self = "picker"
    public static let settings: Self = "settings"
    public static let checklist: Self = "checklist"
    public static let liveList: Self = "liveList"
    public static let detail: Self = "detail"
    public static let confirm: Self = "confirm"
    public static let status: Self = "status"
}

/// A `harness.screen` result: one primitive filled with what the adapter read.
public struct HarnessScreen: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var primitive: HarnessPrimitive
    public var title: String
    public var fetchedAt: WireDate
    /// The source failed and this is the last good copy.
    public var stale: Bool
    public var picker: Picker?
    public var settings: Settings?
    public var checklist: Checklist?
    public var liveList: LiveList?
    public var detail: Detail?
    public var confirm: Confirm?
    public var status: HarnessStatus?
    public var actions: [HarnessActionRef]

    public init(id: String, primitive: HarnessPrimitive, title: String, fetchedAt: WireDate, stale: Bool = false,
                picker: Picker? = nil, settings: Settings? = nil, checklist: Checklist? = nil,
                liveList: LiveList? = nil, detail: Detail? = nil, confirm: Confirm? = nil,
                status: HarnessStatus? = nil, actions: [HarnessActionRef] = []) {
        self.id = id
        self.primitive = primitive
        self.title = title
        self.fetchedAt = fetchedAt
        self.stale = stale
        self.picker = picker
        self.settings = settings
        self.checklist = checklist
        self.liveList = liveList
        self.detail = detail
        self.confirm = confirm
        self.status = status
        self.actions = actions
    }

    /// A searchable, grouped list with one selection. Selecting runs `onSelect` with
    /// args `{"id": item id}`.
    public struct Picker: Codable, Hashable, Sendable {
        public var items: [Item]
        public var selected: String?
        public var searchable: Bool
        public var filters: [Filter]?
        public var onSelect: String
        /// Asked (or applied with Undo) before `onSelect` runs, when set.
        public var onSelectConfirm: HarnessSelectConfirm?
        public var itemActions: [HarnessActionRef]?
        /// What is not set up yet, such as providers not signed in, listed after the items.
        public var setup: [Item]?
        /// Sets up one `setup` entry, run with args `{"id": entry id}`.
        public var setupAction: HarnessActionRef?
        /// "secret": the setup action asks for a value first (an API key), sent as `value`.
        public var setupInput: String?

        public struct Item: Codable, Hashable, Sendable, Identifiable {
            public var id: String
            public var title: String
            public var subtitle: String?
            public var badge: String?
            public var group: String?
            public var disabled: Bool?
            /// Values the filters match, by filter id.
            public var facets: [String: String]?

            public init(id: String, title: String, subtitle: String? = nil, badge: String? = nil, group: String? = nil,
                        disabled: Bool? = nil, facets: [String: String]? = nil) {
                self.id = id
                self.title = title
                self.subtitle = subtitle
                self.badge = badge
                self.group = group
                self.disabled = disabled
                self.facets = facets
            }
        }

        /// `kind` is toggle or choice.
        public struct Filter: Codable, Hashable, Sendable, Identifiable {
            public var id: String
            public var label: String
            public var kind: String
            public var options: [HarnessChoice]?
            public var `default`: String?

            public init(id: String, label: String, kind: String, options: [HarnessChoice]? = nil, default: String? = nil) {
                self.id = id
                self.label = label
                self.kind = kind
                self.options = options
                self.default = `default`
            }
        }

        public init(items: [Item], selected: String? = nil, searchable: Bool, filters: [Filter]? = nil,
                    onSelect: String, onSelectConfirm: HarnessSelectConfirm? = nil, itemActions: [HarnessActionRef]? = nil,
                    setup: [Item]? = nil, setupAction: HarnessActionRef? = nil, setupInput: String? = nil) {
            self.items = items
            self.setup = setup
            self.setupAction = setupAction
            self.setupInput = setupInput
            self.selected = selected
            self.searchable = searchable
            self.filters = filters
            self.onSelect = onSelect
            self.onSelectConfirm = onSelectConfirm
            self.itemActions = itemActions
        }
    }

    /// A form of typed fields. Committing a field runs its `action` with args `{"value": v}`.
    public struct Settings: Codable, Hashable, Sendable {
        public var sections: [Section]

        public struct Section: Codable, Hashable, Sendable {
            public var title: String?
            public var footer: String?
            public var fields: [Field]

            public init(title: String? = nil, footer: String? = nil, fields: [Field]) {
                self.title = title
                self.footer = footer
                self.fields = fields
            }
        }

        public struct FieldKind: OpenString {
            public var rawValue: String
            public init(rawValue: String) { self.rawValue = rawValue }
            public static let toggle: Self = "toggle"
            public static let choice: Self = "choice"
            public static let text: Self = "text"
            public static let number: Self = "number"
            /// Never carries its value: `value` is null and `isSet` says whether one exists.
            public static let secret: Self = "secret"
        }

        public struct Field: Codable, Hashable, Sendable, Identifiable {
            public var id: String
            public var label: String
            public var help: String?
            public var kind: FieldKind
            public var value: JSONValue
            public var isSet: Bool?
            public var options: [HarnessChoice]?
            public var min: Double?
            public var max: Double?
            public var placeholder: String?
            public var readOnly: Bool?
            public var action: String?
            /// Asked (or applied with Undo) before `action` runs, when set.
            public var actionConfirm: HarnessSelectConfirm?

            public init(id: String, label: String, help: String? = nil, kind: FieldKind, value: JSONValue,
                        isSet: Bool? = nil, options: [HarnessChoice]? = nil, min: Double? = nil, max: Double? = nil,
                        placeholder: String? = nil, readOnly: Bool? = nil, action: String? = nil,
                        actionConfirm: HarnessSelectConfirm? = nil) {
                self.id = id
                self.label = label
                self.help = help
                self.kind = kind
                self.value = value
                self.isSet = isSet
                self.options = options
                self.min = min
                self.max = max
                self.placeholder = placeholder
                self.readOnly = readOnly
                self.action = action
                self.actionConfirm = actionConfirm
            }
        }

        public init(sections: [Section]) {
            self.sections = sections
        }
    }

    /// A plan or todo list. Item states are pending, active, done or skipped.
    public struct Checklist: Codable, Hashable, Sendable {
        public var items: [Item]
        public var done: Int
        public var total: Int

        public struct Item: Codable, Hashable, Sendable, Identifiable {
            public var id: String
            public var text: String
            public var state: HarnessState
            public var children: [Item]?

            public init(id: String, text: String, state: HarnessState, children: [Item]? = nil) {
                self.id = id
                self.text = text
                self.state = state
                self.children = children
            }
        }

        public init(items: [Item], done: Int, total: Int) {
            self.items = items
            self.done = done
            self.total = total
        }
    }

    /// Running things. With `refreshSeconds` above 0 the app asks again while it shows.
    public struct LiveList: Codable, Hashable, Sendable {
        public var rows: [HarnessRow]
        public var refreshSeconds: Int

        public init(rows: [HarnessRow], refreshSeconds: Int = 0) {
            self.rows = rows
            self.refreshSeconds = refreshSeconds
        }
    }

    /// A card with a body and actions. `body.kind` is markdown, diff (unified, monospace), kv
    /// or text.
    public struct Detail: Codable, Hashable, Sendable {
        public var subtitle: String?
        public var body: Body
        public var actions: [HarnessActionRef]

        public struct Body: Codable, Hashable, Sendable {
            public var kind: String
            public var text: String?
            public var kv: [HarnessFact]?

            public init(kind: String, text: String? = nil, kv: [HarnessFact]? = nil) {
                self.kind = kind
                self.text = text
                self.kv = kv
            }
        }

        public init(subtitle: String? = nil, body: Body, actions: [HarnessActionRef] = []) {
            self.subtitle = subtitle
            self.body = body
            self.actions = actions
        }
    }

    /// One or more questions, or a plain confirmation when `questions` is empty. Submitting
    /// runs `submit` with args `{"answers": {question id: [option ids or text]}}`.
    public struct Confirm: Codable, Hashable, Sendable {
        public var message: String?
        public var questions: [HarnessQuestion]
        public var destructive: Bool
        public var submit: String

        public init(message: String? = nil, questions: [HarnessQuestion] = [], destructive: Bool = false, submit: String) {
            self.message = message
            self.questions = questions
            self.destructive = destructive
            self.submit = submit
        }
    }
}
