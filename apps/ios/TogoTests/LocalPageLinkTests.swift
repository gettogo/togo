import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct LocalPageLinkTests {
    @Test(arguments: ["http://localhost:5173/", "http://127.0.0.1:3000/login", "https://[::1]:8443/auth"])
    func localPagesNeverUseTheAuthenticationBrowser(_ text: String) throws {
        let url = try #require(URL(string: text))
        #expect(SessionWebLink.destination(url) == .localPreview)
    }

    @Test func providerAndOrdinaryWebPagesKeepTheirRoutes() throws {
        #expect(SessionWebLink.destination(try #require(URL(string: "https://example.com/docs"))) == .web)
        #expect(SessionWebLink.destination(try #require(URL(string: "https://example.com/oauth?redirect_uri=http://localhost:4000/cb"))) == .signIn)
    }

    @Test func forwardKeepsTheRequestedPathQueryAndFragment() throws {
        let source = try #require(URL(string: "https://localhost:8443/a/b?q=one%20two#section"))
        let local = try #require(URL(string: "http://127.0.0.1:50123/"))
        #expect(SessionWebLink.previewURL(source, remote: 8443, local: local).absoluteString
                == "https://127.0.0.1:50123/a/b?q=one%20two#section")
        #expect(SessionWebLink.previewURL(source, remote: 3000, local: local) == local)
    }

    @Test func pageOpensOnlyAListenerListedForItsSession() async throws {
        let fake = FakeReviewServices()
        let port = ListeningPort(port: 5173, address: "127.0.0.1", pid: 1, command: "test")
        fake.portList = [port]
        let subject = ReviewSubject(hostID: UUID(), sessionID: "test", bib: nil, title: "Ports", cwd: "",
                                    managed: false, capabilities: [Capability.ports])
        let model = PortsModel(subject: subject, services: fake)
        defer { model.disappear() }
        await model.openLink(try #require(URL(string: "http://localhost:9999/")))
        #expect(model.shown == nil)
        #expect(model.manualError != nil)
        #expect(!fake.calls.contains(.forward))
        await model.openLink(try #require(URL(string: "http://localhost:5173/page")))
        #expect(model.shown?.remote == 5173)
        #expect(fake.calls.filter { $0 == .forward }.count == 1)
    }
}
