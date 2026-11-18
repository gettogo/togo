import Foundation
import Observation
import TogoKit

/// One screen's search. A newer query or cancelled presentation retires the
/// reply, even when the underlying read completes after cancellation.
@MainActor @Observable final class RemotePickerSearch {
    private(set) var result: HarnessScreen.Picker?
    private(set) var acceptedQuery = ""
    private(set) var loading = false
    private(set) var failed = false
    private var acceptedSource: HarnessScreen.Picker?
    @ObservationIgnored private var request = UUID()

    func isCurrent(query: String, source: HarnessScreen.Picker) -> Bool {
        source.query == query || (!loading && !failed && result != nil && acceptedQuery == query && acceptedSource == source)
    }

    func load(
        query: String, source: HarnessScreen.Picker, delay: Duration = .milliseconds(250),
        read: (@MainActor (String) async throws -> HarnessScreen)?
    ) async {
        let token = UUID()
        request = token
        // A source read already matching this query needs no second request.
        // This also keeps a newly loaded page visible while the view updates.
        if source.searchMode == "remote", source.query == query {
            result = source
            acceptedQuery = query
            acceptedSource = source
            loading = false
            failed = false
            return
        }
        guard source.searchMode == "remote", let read else {
            result = nil
            acceptedQuery = ""
            loading = false
            failed = source.searchMode == "remote"
            return
        }
        loading = true
        failed = false
        defer { if request == token { loading = false } }
        do {
            if !query.isEmpty { try await Task.sleep(for: delay) }
            let screen = try await read(query)
            try Task.checkCancellation()
            guard request == token else { return }
            guard !screen.stale, let picker = screen.picker, picker.searchMode == "remote" else { throw HelperError.malformedResponse }
            result = picker
            acceptedQuery = query
            acceptedSource = source
        } catch {
            guard request == token, !Task.isCancelled else { return }
            failed = true
        }
    }
}
