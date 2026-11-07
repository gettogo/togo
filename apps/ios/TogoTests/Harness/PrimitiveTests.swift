import Foundation
import Testing
import TogoKit
@testable import Togo

/// The native screens' logic: action results and their feedback, Picker filtering and
/// grouping, Settings commits and secrets, Confirm answers and its guards, and the smaller
/// readers behind the Checklist, Live list, Detail card and Status.
@MainActor struct PrimitiveTests {
    // MARK: Actions

    @Test func resultsBecomeFeedback() {
        #expect(PrimitiveFeedback.from(nil) == nil)
        #expect(PrimitiveFeedback.from(.init(result: .applied))?.title == PrimitiveCopy.done)
        let typed = PrimitiveFeedback.from(.init(result: .typed, message: "Sent /model sonnet"))
        #expect(typed?.kind == .sent)
        #expect(typed?.title == PrimitiveCopy.sent)
        #expect(typed?.detail == "Sent /model sonnet")
        #expect(PrimitiveFeedback.from(.init(result: .failed, message: "no such model"))?.title == "no such model")
        #expect(PrimitiveFeedback.from(.init(result: .failed))?.title == PrimitiveCopy.failed)
        #expect(PrimitiveFeedback.from(.init(result: .fallback))?.kind == .terminal)
    }

    @Test func userArgsWinOverFixedOnes() {
        let merged = PrimitiveActions.merged(fixed: ["level": .string("high"), "scope": .string("project")],
                                             user: ["level": .string("low")])
        #expect(merged == ["level": .string("low"), "scope": .string("project")])
        #expect(PrimitiveActions.merged(fixed: nil, user: nil) == nil)
        #expect(PrimitiveActions.merged(fixed: ["a": .null], user: nil) == ["a": .null])
    }

    @Test func performRunsOnceAtATimeAndFallsBackToTheTerminal() async {
        var runs: [String] = []
        var terminal = 0
        let context = PrimitiveContext(
            run: { action, _, _ in
                runs.append(action)
                await Task.yield()
                return HarnessActionResult(result: action == "gone" ? .fallback : .applied)
            },
            openScreen: { _ in }, open: { _ in }, showTerminal: { terminal += 1 }
        )
        let actions = PrimitiveActions()
        let first = Task { await actions.perform("compact", context: context) }
        let second = Task { await actions.perform("compact", context: context) }
        let results = [await first.value, await second.value]
        #expect(runs == ["compact"])
        #expect(results.compactMap { $0 }.count == 1)
        #expect(actions.feedback?.kind == .done)
        #expect(!actions.isBusy)

        await actions.perform("gone", context: context)
        #expect(terminal == 1)
        #expect(actions.feedback?.kind == .terminal)
    }

    @Test func anActionWithAConfirmLineAsksFirst() async {
        var runs: [(String, [String: JSONValue]?)] = []
        let context = PrimitiveContext(
            run: { action, _, args in
                runs.append((action, args))
                return HarnessActionResult(result: .applied)
            },
            openScreen: { _ in }, open: { _ in }, showTerminal: {}
        )
        let actions = PrimitiveActions()
        let ref = HarnessActionRef(id: "stop", label: "Stop", role: .destructive, confirm: "Stop it?", args: ["force": .bool(false)])
        await actions.trigger(ref, target: "dev", context: context)
        #expect(runs.isEmpty)
        #expect(actions.asking?.ref == ref)
        await actions.confirmPending(context: context)
        #expect(runs.map(\.0) == ["stop"])
        #expect(runs.first?.1 == ["force": .bool(false)])
        #expect(actions.asking == nil)
    }

    @Test func refusalsWithFallbackOpenTheTerminal() {
        #expect(HelperError.refused("fallback").isFallback)
        #expect(HelperError.refused("no_adapter").isFallback)
        #expect(!HelperError.refused("needs_admin_setup").isFallback)
        let rpc = RPCErrorObject(code: RPCErrorCode.refused, message: "refused", data: .object(["reason": .string("fallback")]))
        #expect(HelperError.rpc(rpc).isFallback)
        #expect(!HelperError.closed.isFallback)
    }

    // MARK: Picker

    private var models: HarnessScreen.Picker { HarnessSamples.models.picker! }

    @Test func searchMatchesEveryWord() {
        var model = PickerModel(models)
        model.query = "claude son"
        #expect(model.visibleItems(models).map(\.id) == ["anthropic/claude-sonnet-4-5"])
        model.query = "gemini"
        #expect(model.visibleItems(models).count == 2)
        model.query = "openrouter"
        #expect(model.visibleItems(models).count == 2)
    }

    @Test func choiceAndToggleFiltersNarrow() {
        var model = PickerModel(models)
        model.filterValues["kind"] = "reasoning"
        #expect(model.visibleItems(models).map(\.title) == ["GPT-5.1 Codex", "Gemini 3 Pro", "DeepSeek V3.2"])
        model.filterValues["kind"] = PickerModel.allOption
        model.filterValues["free"] = "true"
        #expect(model.visibleItems(models).map(\.title) == ["Qwen3 Coder"])
        #expect(model.hasNarrowing)
        model.reset()
        #expect(model.visibleItems(models).count == models.items.count)
    }

    @Test func groupsKeepTheirOrderAndTotals() {
        var model = PickerModel(models)
        #expect(model.groups(models).map(\.name) == ["anthropic", "openai", "google", "openrouter"])
        model.filterValues["kind"] = "chat"
        let groups = model.groups(models)
        #expect(groups.first { $0.name == "openai" }?.items.count == 1)
        #expect(groups.first { $0.name == "openai" }?.total == 3)
        model.sidebarGroup = "google"
        #expect(model.groups(models).map(\.name) == ["google"])
    }

    @Test func optionCountsApplyTheOtherFilters() throws {
        var model = PickerModel(models)
        let kind = try #require(models.filters?.first)
        #expect(model.count(PickerModel.allOption, of: kind, in: models) == 10)
        #expect(model.count("chat", of: kind, in: models) == 6)
        model.filterValues["free"] = "true"
        #expect(model.count("chat", of: kind, in: models) == 1)
        #expect(PickerModel.options(kind).first?.id == PickerModel.allOption)
    }

    @Test func defaultFilterValuesApply() {
        let picker = HarnessScreen.Picker(
            items: [.init(id: "a", title: "A", facets: ["scope": "project"]), .init(id: "b", title: "B", facets: ["scope": "user"])],
            searchable: true,
            filters: [.init(id: "scope", label: "Scope", kind: "choice", options: [.init(id: "project", label: "Project")], default: "project")],
            onSelect: "pick"
        )
        #expect(PickerModel(picker).visibleItems(picker).map(\.id) == ["a"])
    }

    @Test func foldedGroupsOpenWhileSearching() {
        var model = PickerModel(models)
        let group = model.groups(models)[0]
        model.toggle(group)
        #expect(!model.isExpanded(group))
        model.query = "opus"
        #expect(model.isExpanded(model.groups(models)[0]))
    }

    @Test func roleHoldersComeFromFacets() {
        #expect(PickerModel.holders(of: "default", in: models.items).map(\.title) == ["Claude Sonnet 4.5"])
        let sonnet = models.items[1]
        #expect(PickerModel.held(by: sonnet, actions: models.itemActions ?? []) == ["default", "plan"])
        let facts = PickerModel.facts(of: sonnet, picker: models).map(\.key)
        #expect(!facts.contains("default"))
        #expect(facts.contains("context"))
    }

    @Test func tappingAnItemPicksTheRightThing() {
        var model = PickerModel(models)
        let haiku = models.items[2]
        #expect(model.tap(haiku, in: models) == .details)
        model.picking = "slow"
        #expect(model.tap(haiku, in: models) == .assign(action: "slow"))
        let plain = HarnessSamples.model.picker!
        let picker = PickerModel(plain)
        #expect(picker.tap(plain.items[1], in: plain) == .select)
        #expect(picker.tap(plain.items[0], in: plain) == .none)
        #expect(PickerModel.args(for: plain.items[1]) == ["id": .string("sonnet")])
    }

    // MARK: Settings

    private func field(_ kind: HarnessScreen.Settings.FieldKind, value: JSONValue = .null, isSet: Bool? = nil,
                       min: Double? = nil, max: Double? = nil, readOnly: Bool? = nil, action: String? = "set") -> HarnessScreen.Settings.Field {
        .init(id: "f", label: "Field", kind: kind, value: value, isSet: isSet, min: min, max: max, readOnly: readOnly, action: action)
    }

    @Test func commitSendsOnlyTheValue() {
        #expect(SettingsLogic.commitArgs(.bool(true)) == ["value": .bool(true)])
        #expect(SettingsLogic.commitArgs(.string("high")) == ["value": .string("high")])
        #expect(SettingsLogic.commitArgs(.number(12)) == ["value": .number(12)])
    }

    @Test func onlyFieldsWithAnActionAreEditable() {
        #expect(SettingsLogic.isEditable(field(.toggle)))
        #expect(!SettingsLogic.isEditable(field(.toggle, action: nil)))
        #expect(!SettingsLogic.isEditable(field(.text, readOnly: true)))
    }

    @Test func secretsNeverShowAValue() {
        let leaked = field(.secret, value: .string("sk-ant-should-never-show"), isSet: true)
        #expect(SettingsLogic.initialText(leaked).isEmpty)
        #expect(SettingsLogic.secretStatus(leaked) == PrimitiveCopy.set)
        #expect(SettingsLogic.secretStatus(field(.secret, isSet: false)) == PrimitiveCopy.notSet)
        #expect(SettingsLogic.secretStatus(field(.secret)) == PrimitiveCopy.notSet)
    }

    @Test func numbersClampAndRead() {
        let turns = field(.number, value: .number(24), min: 1, max: 50)
        #expect(SettingsLogic.clamp(80, turns) == 50)
        #expect(SettingsLogic.clamp(-2, turns) == 1)
        #expect(SettingsLogic.step(turns) == 1)
        #expect(SettingsLogic.step(field(.number, min: 0, max: 2)) == 1)
        #expect(SettingsLogic.step(field(.number, min: 0, max: 1.5)) == 0.1)
        #expect(SettingsLogic.parse("99", for: turns) == .number(50))
        #expect(SettingsLogic.parse("many", for: turns) == nil)
        #expect(SettingsLogic.numberText(24) == "24")
        #expect(SettingsLogic.numberText(0.25) == "0.25")
        #expect(SettingsLogic.number(.string("7")) == 7)
    }

    @Test func choicesReadTheirLabels() {
        let effort = HarnessSamples.settings.settings!.sections[0].fields[1]
        #expect(SettingsLogic.choiceLabel(effort, value: .string("high")) == "High")
        #expect(SettingsLogic.choiceLabel(effort, value: .string("max")) == "max")
        #expect(SettingsLogic.showsInline(effort))
        #expect(!SettingsLogic.showsInline(HarnessSamples.settings.settings!.sections[0].fields[0]))
    }

    // MARK: Confirm

    private var questions: HarnessScreen.Confirm { HarnessSamples.question.confirm! }

    private func armed(_ confirm: HarnessScreen.Confirm) async -> ConfirmModel {
        let model = ConfirmModel(confirm: confirm, armDelay: .zero)
        await model.arming?.value
        return model
    }

    @Test func answersMapQuestionsToOptionsAndText() async {
        let model = await armed(questions)
        let env = questions.questions[0], checks = questions.questions[1]
        model.toggle("prod", in: env)
        model.toggle("staging", in: env)
        model.toggle("e2e", in: checks)
        model.toggle("unit", in: checks)
        model.setText("  skip lint  ", for: checks)
        #expect(model.answers == ["env": ["staging"], "checks": ["unit", "e2e", "skip lint"]])
        #expect(ConfirmModel.args(model.answers) == ["answers": .object([
            "env": .array([.string("staging")]),
            "checks": .array([.string("unit"), .string("e2e"), .string("skip lint")]),
        ])])
        model.toggle("staging", in: env)
        #expect(!model.isComplete)
    }

    @Test func aPlainConfirmSendsEmptyAnswers() async {
        let model = await armed(HarnessSamples.compact.confirm!)
        #expect(model.isComplete)
        var sent: [String: JSONValue]?
        await model.submit(for: HarnessSamples.compact.confirm!) { args in
            sent = args
            return HarnessActionResult(result: .applied)
        }
        #expect(sent == ["answers": .object([:])])
        #expect(model.phase == .answered(nil))
    }

    @Test func answersWaitForTheArmingDelay() async {
        let model = ConfirmModel(confirm: HarnessSamples.compact.confirm!, armDelay: .seconds(60))
        #expect(!model.canSubmit)
        var sends = 0
        await model.submit(for: HarnessSamples.compact.confirm!) { _ in
            sends += 1
            return HarnessActionResult(result: .applied)
        }
        #expect(sends == 0)
        model.arming?.cancel()
    }

    @Test func anAnswerForAReplacedQuestionIsDropped() async {
        let model = await armed(questions)
        model.toggle("staging", in: questions.questions[0])
        model.toggle("unit", in: questions.questions[1])
        var replaced = questions
        replaced.message = "One more thing."
        model.update(replaced)
        #expect(model.changed)
        #expect(model.answers.isEmpty)
        #expect(!model.armed)
        var sends = 0
        await model.submit(for: questions) { _ in
            sends += 1
            return HarnessActionResult(result: .applied)
        }
        #expect(sends == 0)
    }

    @Test func theSameQuestionKeepsItsAnswers() async {
        let model = await armed(questions)
        model.toggle("staging", in: questions.questions[0])
        model.update(questions)
        #expect(model.answers == ["env": ["staging"]])
        #expect(model.armed)
    }

    @Test func aFailedAnswerReopensTheQuestion() async {
        let model = await armed(HarnessSamples.compact.confirm!)
        await model.submit(for: HarnessSamples.compact.confirm!) { _ in HarnessActionResult(result: .failed, message: "busy") }
        #expect(model.phase == .failed("busy"))
        await model.arming?.value
        #expect(model.canSubmit)
        await model.submit(for: HarnessSamples.compact.confirm!) { _ in nil }
        #expect(model.phase == .open)
    }

    // MARK: Checklist, live list, detail, status

    @Test func checklistFlattensWithDepth() {
        let items = HarnessSamples.plan.checklist!.items
        let flat = ChecklistLogic.flatten(items)
        #expect(flat.map(\.item.id) == ["1", "2", "2a", "2b", "3", "4", "5"])
        #expect(flat.map(\.depth) == [0, 0, 1, 1, 0, 0, 0])
        #expect(ChecklistLogic.current(items)?.id == "3")
        #expect(ChecklistLogic.fraction(done: 2, total: 5) == 0.4)
        #expect(ChecklistLogic.fraction(done: 3, total: 0) == 0)
    }

    @Test func liveListSummarisesWithSpaces() {
        let rows = HarnessSamples.jobs.liveList!.rows
        #expect(LiveListLogic.summary(rows) == "2 running  1 done  1 failed")
        let long = HarnessRow(id: "x", title: "x", state: .running, tail: ["1", "2", "3", "4", "5", "6"])
        #expect(LiveListLogic.tail(long) == ["3", "4", "5", "6"])
        #expect(LiveListLogic.summary([]).isEmpty)
    }

    @Test func diffReadsFilesAndCounts() throws {
        let files = PrimitiveDiffLogic.files(HarnessSamples.patchText)
        let file = try #require(files.first)
        #expect(files.count == 1)
        #expect(file.path == "src/date.ts")
        #expect(file.added == 3)
        #expect(file.removed == 2)
        #expect(file.lines.first?.kind == .hunk)
        #expect(file.lines.filter { $0.kind == .added }.map(\.number) == [13, 15, 16])
    }

    @Test func hunkOnlyDiffStillDraws() {
        let files = PrimitiveDiffLogic.files("@@ -1,2 +1,2 @@\n-old line\n+new line\n same\n")
        #expect(files.count == 1)
        #expect(files[0].added == 1)
        #expect(files[0].removed == 1)
        #expect(files[0].lines.map(\.kind) == [.hunk, .removed, .added, .context])
    }

    @Test func markdownSplitsIntoBlocks() {
        let blocks = PrimitiveMarkdownBlock.parse("## Title\nSome **bold** text\n\n- one\n1. two\n```sh\nnpm test\n```\n> note\n---")
        #expect(blocks == [
            .heading(level: 2, text: "Title"),
            .paragraph("Some **bold** text"),
            .item(marker: "\u{2022}", text: "one"),
            .item(marker: "1.", text: "two"),
            .code(language: "sh", text: "npm test"),
            .quote("note"),
            .rule,
        ])
    }

    @Test func detailBodiesFallBackToText() {
        #expect(DetailCardLogic.kind(.init(kind: "diff", text: "x")) == .diff)
        #expect(DetailCardLogic.kind(.init(kind: "kv", kv: [])) == .empty)
        #expect(DetailCardLogic.kind(.init(kind: "html", text: "<b>")) == .text)
        let refs: [HarnessActionRef] = [.init(id: "a", label: "A", role: .primary), .init(id: "b", label: "B"),
                                        .init(id: "c", label: "C", role: .destructive)]
        #expect(DetailCardLogic.ordered(refs).map(\.id) == ["c", "b", "a"])
    }

    @Test func statusLevelsAndCells() {
        #expect(StatusLogic.level(40) == .normal)
        #expect(StatusLogic.level(86) == .high)
        #expect(StatusLogic.level(97) == .full)
        #expect(StatusLogic.percentText(140) == "100%")
        #expect(StatusLogic.fraction(-5) == 0)
        #expect(StatusLogic.cells(HarnessSamples.usage.status!).map(\.label) ==
            [PrimitiveCopy.model, PrimitiveCopy.mode, PrimitiveCopy.effort, PrimitiveCopy.cost])
    }

    @Test func screensPickTheirPayloadAndTheNewestCopy() {
        if case .unknown = PrimitiveScreenLogic.payload(HarnessScreen(id: "x", primitive: "timeline", title: "T", fetchedAt: HarnessSamples.at)) {
        } else {
            Issue.record("an unknown primitive should hand off to the terminal")
        }
        if case .unknown = PrimitiveScreenLogic.payload(HarnessScreen(id: "x", primitive: .picker, title: "T", fetchedAt: HarnessSamples.at)) {
        } else {
            Issue.record("a picker without its payload should hand off to the terminal")
        }
        var newer = HarnessSamples.model
        newer.fetchedAt = WireDate(HarnessSamples.at.date.addingTimeInterval(5))
        newer.picker?.selected = "haiku"
        #expect(PrimitiveScreenLogic.newest(HarnessSamples.model, newer).picker?.selected == "haiku")
        #expect(PrimitiveScreenLogic.newest(newer, HarnessSamples.model).picker?.selected == "haiku")
        #expect(PrimitiveScreenLogic.newest(HarnessSamples.model, nil) == HarnessSamples.model)
    }

    @Test func previewServicesApplySelections() async throws {
        let services = PreviewHarnessServices()
        let store = HarnessSessionStore.preview(services)
        let result = try await store.run(action: "set_model", target: "sonnet", args: ["id": .string("sonnet")])
        #expect(result.screen?.picker?.selected == "sonnet")
        let key = try await store.run(action: "set_key", target: "azure.key", args: ["value": .string("secret")])
        let azure = key.screen?.settings?.sections[2].fields[1]
        #expect(azure?.isSet == true)
        #expect(azure?.value == .null)
    }
}
