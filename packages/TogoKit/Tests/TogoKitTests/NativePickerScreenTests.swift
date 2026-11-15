import Foundation
import Testing
@testable import TogoKit

/// The program's own pickers, recorded from OMP 18.20 in isolated sessions
/// (paths shortened, lists trimmed, shapes untouched), mapped to the shared
/// picker screen.
struct NativePickerScreenTests {
    static let at = WireDate(Date(timeIntervalSince1970: 1_800_000_000))

    static func node(_ json: String) throws -> SurfaceNode {
        try JSONDecoder().decode(SurfaceNode.self, from: Data(json.utf8))
    }

    @Test func conversationPickerKeepsRowsGroupsPreviewAndRowActions() throws {
        let screen = try NativePickerScreen.map(try Self.node(Self.resume), workflow: "resume", fallbackTitle: "Resume", fetchedAt: Self.at)
        let picker = try #require(screen.picker)
        #expect(screen.primitive == .picker)
        #expect(screen.id == "n")
        #expect(screen.title == "Resume")
        #expect(picker.items.map(\.title) == ["Native check A"])
        let item = try #require(picker.items.first)
        #expect(item.id == "/sessions/project/a.jsonl")
        #expect(item.subtitle == "Reply with only these four words: Native check A ready")
        #expect(item.group == "Today")
        #expect(item.facets?["size"] == "3.2KB")
        #expect(item.facets?["when"] == "now")
        #expect(item.facets?["native.dot"] == "success")
        // In use now is the session's own file, which is not a row: nothing
        // is marked selected, and the cursor is never taken for it.
        #expect(picker.selected == nil)
        #expect(picker.onSelect == NativePickerScreen.activate)
        #expect(picker.onSelectLabel == "Resume")
        #expect(picker.searchable)
        #expect(picker.searchMode == "remote")
        #expect(picker.selectionBehavior == "preview")
        #expect(picker.itemActions == nil)
        let row = try #require(picker.contextActions)
        #expect(row.map(\.id) == ["native.action.delete", "native.action.rename", "native.action.pin"])
        #expect(row.map(\.label) == ["Delete", "Rename", "Pin"])
        #expect(row.map(\.role) == [.destructive, .normal, .normal])
        #expect(row[0].args == ["act": .string("delete")])
        // The list-wide action; Close is the system's, Resume is the choice.
        #expect(screen.actions.map(\.id) == ["native.action.scope"])
        #expect(screen.actions.first?.label == "All projects")
        let preview = try #require(item.preview)
        #expect(preview.subtitle == "Native check A")
        #expect(preview.body.kind == "kv")
        #expect(preview.body.kv?.map(\.label) == ["Folder", "Created", "Modified", "Size", "Status"])
        #expect(preview.body.kv?.first?.value == "/tmp/001")
        #expect(preview.body.kv?.last?.value == "done")
        let sections = try #require(preview.sections)
        #expect(sections.map(\.title) == ["Conversation", nil])
        #expect(sections.map(\.body.kind) == ["markdown", "markdown"])
        #expect(sections.map(\.body.text) == ["Reply with only these four words: Native check A ready", "Native check A ready"])
    }

    @Test func aMarkedCurrentRowIsSelected() throws {
        var json = Self.resume
        json = json.replacingOccurrences(of: "/sessions/project/current.jsonl", with: "/sessions/project/a.jsonl")
        let screen = try NativePickerScreen.map(try Self.node(json), workflow: "resume", fallbackTitle: "Resume", fetchedAt: Self.at)
        #expect(screen.picker?.selected == "/sessions/project/a.jsonl")
    }

    @Test func signInPickerHasNoRowActionsAndUsesTheProgramsTitle() throws {
        let screen = try NativePickerScreen.map(try Self.node(Self.login), workflow: "login", fallbackTitle: "Sign in", fetchedAt: Self.at)
        let picker = try #require(screen.picker)
        #expect(picker.items.count == 3)
        #expect(picker.onSelectLabel == "Sign in")
        #expect(picker.contextActions == nil)
        #expect(screen.actions.isEmpty)
        #expect(picker.selected == nil)
        #expect(picker.items.allSatisfy({ $0.preview == nil }))
        #expect(!screen.title.isEmpty)
    }

    @Test func modelPickerKeepsGroupsBadgesTabsAndScopes() throws {
        let screen = try NativePickerScreen.map(try Self.node(Self.model), workflow: "model", fallbackTitle: "Model", fetchedAt: Self.at)
        let picker = try #require(screen.picker)
        #expect(picker.items.count == 4)
        #expect(picker.items.first?.group == "Recent")
        #expect(picker.items.first?.badge == "free")
        #expect(picker.items.first?.facets?["native.mono"] == "true")
        #expect(picker.onSelectLabel == "Assign role")
        let tabs = screen.actions.filter({ $0.id.hasPrefix(NativePickerScreen.tabPrefix) })
        let scopes = screen.actions.filter({ $0.id.hasPrefix(NativePickerScreen.scopePrefix) })
        #expect(tabs.first?.id == "native.tab.all")
        #expect(tabs.first?.args?["selected"] == .bool(true))
        #expect(tabs.dropFirst().allSatisfy({ $0.args?["selected"] == .bool(false) }))
        #expect(scopes.count == 3)
        #expect(scopes.contains(where: { $0.args?["selected"] == .bool(true) }))
        let preview = try #require(picker.items.first(where: { $0.preview != nil })?.preview)
        #expect(preview.subtitle == "Parallel")
        #expect(preview.body.kv?.isEmpty == false)
    }

    /// Nothing is guessed: a picker whose extra actions are not classified
    /// for the workflow is refused, naming them.
    @Test func unclassifiedActionsAreRefused() throws {
        #expect(throws: NativePickerScreen.Unsupported.unknownActions(workflow: "tree", actions: ["summarize", "label", "filter"])) {
            try NativePickerScreen.map(try Self.node(Self.tree), workflow: "tree", fallbackTitle: "Tree", fetchedAt: Self.at)
        }
        #expect(throws: NativePickerScreen.Unsupported.unknownActions(workflow: "other", actions: ["delete", "rename", "pin", "scope"])) {
            try NativePickerScreen.map(try Self.node(Self.resume), workflow: "other", fallbackTitle: "", fetchedAt: Self.at)
        }
    }

    @Test func otherNodesAndNestedControlsAreRefused() throws {
        #expect(throws: NativePickerScreen.Unsupported.notAPicker(kind: "prefs")) {
            try NativePickerScreen.map(try Self.node(#"{"id":"s","k":"prefs"}"#), workflow: "settings", fallbackTitle: "", fetchedAt: Self.at)
        }
        let rename = #"{"id":"n","k":"picker","p":{"items":[],"actions":[]},"c":[{"id":"n.r","k":"input","p":{"text":"Native check A"}}]}"#
        #expect(throws: NativePickerScreen.Unsupported.nestedControl(kind: "input")) {
            try NativePickerScreen.map(try Self.node(rename), workflow: "resume", fallbackTitle: "", fetchedAt: Self.at)
        }
        let nameless = #"{"id":"n","k":"picker","p":{"items":[{"id":"x"}]}}"#
        #expect(throws: NativePickerScreen.Unsupported.malformedItem) {
            try NativePickerScreen.map(try Self.node(nameless), workflow: "login", fallbackTitle: "", fetchedAt: Self.at)
        }
    }

    static let resume = #"""
{
 "id": "n",
 "k": "picker",
 "p": {
  "actions": [
   {
    "id": "resume",
    "keys": [
     "enter"
    ],
    "label": "Resume",
    "primary": true
   },
   {
    "id": "delete",
    "keys": [
     "backspace"
    ],
    "label": "Delete"
   },
   {
    "id": "rename",
    "keys": [
     "f2"
    ],
    "label": "Rename"
   },
   {
    "id": "pin",
    "keys": [
     "alt",
     "k"
    ],
    "label": "Pin"
   },
   {
    "id": "scope",
    "keys": [
     "tab"
    ],
    "label": "All projects"
   },
   {
    "end": true,
    "id": "close",
    "keys": [
     "esc"
    ],
    "label": "Close"
   }
  ],
  "columns": [
   {
    "format": "time",
    "id": "when"
   },
   {
    "format": "dim",
    "id": "size"
   }
  ],
  "current": [
   "/sessions/project/current.jsonl"
  ],
  "cursor": 0,
  "empty": "No sessions in this folder yet",
  "icon": "history",
  "items": [
   {
    "detail": "Reply with only these four words: Native check A ready",
    "dot": "success",
    "facts": {
     "size": "3.2KB",
     "when": "now"
    },
    "id": "/sessions/project/a.jsonl",
    "label": "Native check A",
    "title": "14 Nov 2026, 19:14"
   }
  ],
  "layout": "cards",
  "noun": "sessions",
  "order": [
   {
    "count": 1,
    "group": "today-0",
    "label": "Today"
   },
   "/sessions/project/a.jsonl"
  ],
  "placeholder": "Search sessions in 001…",
  "preview": "side",
  "query": "",
  "selected": "/sessions/project/a.jsonl",
  "size": "lg",
  "state": "ready",
  "total": 1
 },
 "c": [
  {
   "id": "n.0",
   "k": "text",
   "p": {
    "role": "omp.picker.title",
    "text": "Native check A"
   }
  },
  {
   "id": "n.1",
   "k": "kv",
   "p": {
    "items": [
     {
      "k": [
       {
        "s": "muted",
        "t": "Folder"
       }
      ],
      "v": [
       {
        "s": "path dim",
        "t": "/tmp/"
       },
       {
        "s": "path",
        "t": "001"
       }
      ]
     },
     {
      "k": [
       {
        "s": "muted",
        "t": "Created"
       }
      ],
      "v": "14 Nov 2026, 16:54"
     },
     {
      "k": [
       {
        "s": "muted",
        "t": "Modified"
       }
      ],
      "v": "14 Nov 2026, 19:14"
     },
     {
      "k": [
       {
        "s": "muted",
        "t": "Size"
       }
      ],
      "v": "3.2KB"
     },
     {
      "k": [
       {
        "s": "muted",
        "t": "Status"
       }
      ],
      "v": [
       {
        "s": "success",
        "t": "done"
       }
      ]
     }
    ]
   }
  },
  {
   "id": "n.2",
   "k": "section",
   "p": {
    "head": "Conversation"
   },
   "c": [
    {
     "id": "n.2/0",
     "k": "md",
     "p": {
      "text": "Reply with only these four words: Native check A ready",
      "tone": "user"
     }
    },
    {
     "id": "n.2/1",
     "k": "md",
     "p": {
      "text": "Native check A ready"
     }
    }
   ]
  }
 ]
}
"""#

    static let login = #"""
{
 "id": "n.^picker",
 "k": "picker",
 "p": {
  "actions": [
   {
    "id": "confirm",
    "keys": [
     "enter"
    ],
    "label": "Sign in",
    "primary": true
   },
   {
    "end": true,
    "id": "close",
    "keys": [
     "esc"
    ],
    "label": "Close"
   }
  ],
  "cursor": 0,
  "empty": "No OAuth providers available",
  "icon": "key-round",
  "items": [
   {
    "detail": "Not configured",
    "dot": "muted",
    "id": "openai-codex",
    "label": "ChatGPT Plus/Pro (Codex Subscription)",
    "mark": {
     "seed": "openai-codex",
     "text": "CP"
    }
   },
   {
    "detail": "Not configured",
    "dot": "muted",
    "id": "anthropic",
    "label": "Anthropic (Claude Pro/Max)",
    "mark": {
     "seed": "anthropic",
     "text": "AC"
    }
   },
   {
    "detail": "Not configured",
    "dot": "muted",
    "id": "zai",
    "label": "Z.AI (GLM Coding Plan)",
    "mark": {
     "seed": "zai",
     "text": "ZA"
    }
   }
  ],
  "layout": "cards",
  "noun": "providers",
  "placeholder": "Search providers…",
  "preview": "none",
  "query": "",
  "selected": "openai-codex",
  "size": "md",
  "subtitle": "Pick a provider",
  "title": "Sign in",
  "total": 3
 }
}
"""#

    static let model = #"""
{
 "id": "n",
 "k": "picker",
 "p": {
  "actions": [
   {
    "id": "assign",
    "keys": [
     "enter"
    ],
    "label": "Assign role",
    "primary": true
   },
   {
    "end": true,
    "id": "close",
    "keys": [
     "esc"
    ],
    "label": "Close"
   }
  ],
  "columns": [
   {
    "format": "num",
    "head": "Int",
    "id": "int",
    "priority": 1
   },
   {
    "format": "num",
    "head": "t/s",
    "id": "speed",
    "priority": 2
   },
   {
    "format": "num",
    "head": "Ctx",
    "id": "ctx",
    "priority": 4
   },
   {
    "format": "price",
    "head": "$/M",
    "id": "price",
    "priority": 3
   }
  ],
  "cursor": 0,
  "empty": "No models available in this scope",
  "focus": "scopes",
  "icon": "cpu",
  "items": [
   {
    "badges": [
     {
      "text": "free",
      "tone": "success"
     }
    ],
    "chips": [
     {
      "auto": true,
      "on": false,
      "text": "web"
     }
    ],
    "facts": {
     "price": "free"
    },
    "id": "web/parallel",
    "label": "web/parallel",
    "mark": {
     "icon": "provider:web",
     "seed": "web",
     "text": "We"
    },
    "mono": true
   },
   {
    "badges": [
     {
      "text": "free",
      "tone": "success"
     }
    ],
    "chips": [
     {
      "auto": true,
      "on": false,
      "text": "speech"
     }
    ],
    "facts": {
     "price": "free"
    },
    "id": "local/kokoro",
    "label": "local/kokoro",
    "mark": {
     "icon": "provider:local",
     "seed": "local",
     "text": "Lo"
    },
    "mono": true
   },
   {
    "badges": [
     {
      "text": "free",
      "tone": "success"
     }
    ],
    "chips": [
     {
      "auto": true,
      "on": false,
      "text": "dictation"
     }
    ],
    "facts": {
     "price": "free"
    },
    "id": "local/parakeet-tdt-0.6b-v3",
    "label": "local/parakeet-tdt-0.6b-v3",
    "mark": {
     "icon": "provider:local",
     "seed": "local",
     "text": "Lo"
    },
    "mono": true
   },
   {
    "badges": [
     {
      "text": "free",
      "tone": "success"
     }
    ],
    "facts": {
     "ctx": 8192,
     "price": "free"
    },
    "id": "apple/on-device",
    "label": "apple/on-device",
    "mark": {
     "icon": "provider:apple",
     "seed": "apple",
     "text": "Ap"
    },
    "mono": true
   }
  ],
  "layout": "rows",
  "noun": "models",
  "order": [
   {
    "count": 3,
    "group": "recent",
    "label": "Recent"
   },
   "web/parallel",
   "local/kokoro",
   "local/parakeet-tdt-0.6b-v3",
   {
    "count": 1,
    "group": "provider:apple:0",
    "label": "apple"
   },
   "apple/on-device",
   {
    "count": 12,
    "group": "provider:local:1",
    "label": "local"
   }
  ],
  "placeholder": "Search models…",
  "preview": "side",
  "query": "",
  "scope": "all",
  "scopes": [
   {
    "count": 16,
    "icon": "sparkles",
    "id": "roles",
    "label": "Roles"
   },
   {
    "count": 40,
    "icon": "list",
    "id": "all",
    "label": "All models"
   },
   {
    "count": 1,
    "dot": "success",
    "group": "Providers",
    "id": "provider:apple",
    "label": "apple",
    "mark": {
     "icon": "provider:apple",
     "seed": "apple",
     "text": "Ap"
    }
   }
  ],
  "selected": "web/parallel",
  "size": "lg",
  "state": "ready",
  "tab": "all",
  "tabs": [
   {
    "count": 40,
    "id": "all",
    "label": "All"
   },
   {
    "count": 1,
    "id": "chat",
    "label": "Chat"
   },
   {
    "count": 8,
    "id": "tiny",
    "label": "Tiny"
   },
   {
    "count": 0,
    "id": "image",
    "label": "Image"
   },
   {
    "count": 1,
    "id": "tts",
    "label": "TTS"
   },
   {
    "count": 5,
    "id": "stt",
    "label": "STT"
   },
   {
    "count": 25,
    "id": "search",
    "label": "Search"
   },
   {
    "count": 0,
    "id": "judge",
    "label": "Judge"
   },
   {
    "count": 0,
    "id": "embedding",
    "label": "Embedding"
   },
   {
    "count": 0,
    "id": "rerank",
    "label": "Rerank"
   },
   {
    "count": 0,
    "id": "video",
    "label": "Video"
   }
  ],
  "title": "Models",
  "total": 40
 },
 "c": [
  {
   "id": "n.0",
   "k": "text",
   "p": {
    "role": "omp.picker.title",
    "text": "Parallel"
   }
  },
  {
   "id": "n.1",
   "k": "text",
   "p": {
    "actions": {
     "click": "copy"
    },
    "spans": [
     {
      "s": "mono",
      "t": "web/parallel"
     }
    ],
    "title": "Copy model id",
    "truncate": "middle"
   }
  },
  {
   "id": "n.2",
   "k": "kv",
   "p": {
    "items": [
     {
      "k": [
       {
        "s": "muted",
        "t": "Compacts at"
       }
      ],
      "v": [
       {
        "s": "mono",
        "t": "off"
       },
       {
        "s": "dim",
        "t": " · default"
       }
      ]
     },
     {
      "k": [
       {
        "s": "muted",
        "t": "Price"
       }
      ],
      "v": [
       {
        "s": "mono",
        "t": "free"
       }
      ]
     },
     {
      "k": [
       {
        "s": "muted",
        "t": "Input"
       }
      ],
      "v": [
       {
        "s": "mono",
        "t": "text"
       }
      ]
     }
    ]
   }
  },
  {
   "id": "n.3",
   "k": "section",
   "p": {
    "head": "Roles"
   },
   "c": [
    {
     "id": "n.3/0",
     "k": "row",
     "p": {
      "gap": "xs",
      "wrap": true
     },
     "c": [
      {
       "id": "n.3/0/0",
       "k": "badge",
       "p": {
        "text": "web",
        "title": "Web search (auto-selected)",
        "tone": "muted"
       }
      }
     ]
    }
   ]
  }
 ]
}
"""#

    static let tree = #"""
{
 "id": "n.^picker",
 "k": "picker",
 "p": {
  "actions": [
   {
    "id": "switch",
    "keys": [
     "enter"
    ],
    "label": "Switch",
    "primary": true
   },
   {
    "id": "summarize",
    "keys": [
     "shift",
     "enter"
    ],
    "label": "Summarize & switch"
   },
   {
    "id": "label",
    "keys": [
     "shift",
     "l"
    ],
    "label": "Label"
   },
   {
    "id": "filter",
    "keys": [
     "ctrl",
     "o"
    ],
    "label": "Filter"
   },
   {
    "end": true,
    "id": "close",
    "keys": [
     "esc"
    ],
    "label": "Close"
   }
  ],
  "current": [
   "5843eb7d"
  ],
  "cursor": 0,
  "empty": [
   {
    "s": "muted",
    "t": "1 entries hidden by the current filter [default]. Press "
   },
   {
    "s": "key",
    "t": "alt+a"
   },
   {
    "s": "muted",
    "t": " to show all, "
   },
   {
    "s": "key",
    "t": "alt+d"
   },
   {
    "s": "muted",
    "t": " for default"
   }
  ],
  "focus": "list",
  "icon": "git-branch",
  "items": [
   {
    "depth": 0,
    "dot": "accent",
    "id": "5843eb7d",
    "label": [
     {
      "s": "dim",
      "t": "[thinking: high]"
     }
    ],
    "node": "marker"
   }
  ],
  "layout": "tree",
  "noun": "entries",
  "order": [],
  "placeholder": "Search entries…",
  "preview": "side",
  "query": "",
  "size": "lg",
  "tab": "default",
  "tabs": [
   {
    "id": "default",
    "label": "Default"
   },
   {
    "id": "no-tools",
    "label": "No tools"
   },
   {
    "id": "user-only",
    "label": "User only"
   },
   {
    "id": "labeled-only",
    "label": "Labeled"
   },
   {
    "id": "all",
    "label": "All"
   }
  ],
  "title": "Session tree",
  "total": 1
 }
}
"""#
}
