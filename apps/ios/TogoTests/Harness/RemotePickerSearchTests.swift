import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct RemotePickerSearchTests {
    private var source: HarnessScreen.Picker {
        .init(items: [.init(id: "recent", title: "Recent")], searchable: true,
              onSelect: "resume", searchMode: "remote")
    }

    private func screen(_ id: String) -> HarnessScreen {
        .init(id: "resume", primitive: .picker, title: "Resume", fetchedAt: WireDate(Date()),
              picker: .init(items: [.init(id: id, title: "Server found body text")],
                            searchable: true, onSelect: "resume", searchMode: "remote"))
    }

    @Test func serverSearchOwnsMatchingAndReceivesLiteralQuery() async {
        let search = RemotePickerSearch()
        let query = "100%! <div> /resume"
        var read: String?
        await search.load(query: query, source: source, delay: .zero) { value in
            read = value
            return screen("older")
        }
        #expect(read == query)
        #expect(search.isCurrent(query: query, source: source))
        var local = PickerModel(source)
        local.query = query
        let result = search.result ?? source
        #expect(local.visibleItems(result).map(\.id) == ["older"])
        var legacy = result
        legacy.searchMode = nil
        #expect(local.visibleItems(legacy).isEmpty)
    }

    @Test func lateSearchCannotReplaceNewerResults() async throws {
        let search = RemotePickerSearch()
        let (gate, continuation) = AsyncStream<Void>.makeStream()
        var entered = false
        let old = Task {
            await search.load(query: "old", source: source, delay: .zero) { _ in
                entered = true
                for await _ in gate { break }
                return screen("old")
            }
        }
        let deadline = ContinuousClock.now + .seconds(2)
        while !entered, ContinuousClock.now < deadline { await Task.yield() }
        #expect(entered)
        await search.load(query: "new", source: source, delay: .zero) { _ in screen("new") }
        continuation.yield(())
        continuation.finish()
        await old.value
        #expect(search.result?.items.first?.id == "new")
        #expect(search.isCurrent(query: "new", source: source))
        #expect(!search.isCurrent(query: "old", source: source))
    }

    @Test func failedQueryCannotOfferOldResultsAsCurrent() async {
        let search = RemotePickerSearch()
        await search.load(query: "", source: source, delay: .zero) { _ in screen("recent") }
        await search.load(query: "missing", source: source, delay: .zero) { _ in throw HelperError.closed }
        #expect(search.failed)
        #expect(!search.loading)
        #expect(!search.isCurrent(query: "missing", source: source))
    }

    @Test func legacyPickerNeverCallsRemoteSearch() async {
        var legacy = source
        legacy.searchMode = nil
        var calls = 0
        let search = RemotePickerSearch()
        await search.load(query: "local", source: legacy, delay: .zero) { _ in
            calls += 1
            return screen("unexpected")
        }
        #expect(calls == 0)
        #expect(!search.failed)
    }

    @Test func cachedScreenCannotPretendToMatchANewServerQuery() async {
        let search = RemotePickerSearch()
        await search.load(query: "new", source: source, delay: .zero) { _ in
            var cached = screen("cached")
            cached.stale = true
            return cached
        }
        #expect(search.failed)
        #expect(search.result == nil)
        #expect(!search.isCurrent(query: "new", source: source))
    }

    @Test func cancellationRetiresAnOtherwiseSuccessfulRead() async {
        let search = RemotePickerSearch()
        let (gate, continuation) = AsyncStream<Void>.makeStream()
        var entered = false
        let reading = Task {
            await search.load(query: "cancelled", source: source, delay: .zero) { _ in
                entered = true
                for await _ in gate { break }
                return screen("cancelled")
            }
        }
        let deadline = ContinuousClock.now + .seconds(2)
        while !entered, ContinuousClock.now < deadline { await Task.yield() }
        #expect(entered)
        reading.cancel()
        continuation.finish()
        await reading.value
        #expect(search.result == nil)
        #expect(!search.loading)
        #expect(!search.failed)
    }
}
