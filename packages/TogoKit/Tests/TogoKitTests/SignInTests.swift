import Foundation
import Testing
import TogoKit

struct SignInTests {
    private func approval(_ signIn: String) throws -> Approval {
        let json = """
            {"requestId":"r_abcdefghij","sessionId":"s_abcdefghij","bib":"01","agent":"custom",\
            "source":"screen","title":"Confirm code","detail":null,"snippet":"","options":[],\
            "allowText":false,"signIn":\(signIn),"createdAt":"2026-11-07T12:00:01Z",\
            "expiresAt":null,"state":"pending"}
            """
        return try JSONDecoder().decode(Approval.self, from: Data(json.utf8))
    }

    @Test func decodesPageAndCode() throws {
        let a = try approval(#"{"url":"https://ampcode.com/auth/cli/device?user_code=QRLJ-CBJZ","code":"QRLJ-CBJZ"}"#)
        #expect(a.signIn?.url?.host() == "ampcode.com")
        #expect(a.signIn?.code == "QRLJ-CBJZ")
        #expect(a.signIn?.secret == false)
    }

    @Test func dropsPagesThatAreNotWeb() throws {
        let a = try approval(#"{"url":"file:///etc/passwd","secret":true}"#)
        #expect(a.signIn?.url == nil)
        #expect(a.signIn?.secret == true)
    }

    @Test func malformedSignInKeepsTheApproval() throws {
        let a = try approval(#""nope""#)
        #expect(a.signIn == nil)
        #expect(a.title == "Confirm code")
    }

    @Test func roundTrips() throws {
        let a = try approval(#"{"url":"https://github.com/login/device","code":"0B40-4284"}"#)
        let back = try JSONDecoder().decode(Approval.self, from: JSONEncoder().encode(a))
        #expect(back.signIn == a.signIn)
    }

    @Test func catalogStartsMutedCustomSessions() {
        for command in SignInCatalog.all {
            let params = SignInCatalog.params(for: command)
            #expect(params.agent == "custom")
            #expect(params.muted == true)
            #expect(params.program == command.program)
            #expect((params.name ?? "").count <= 64)
        }
        #expect(Set(SignInCatalog.all.map(\.id)).count == SignInCatalog.all.count)
    }

    @Test func catalogFiltersByInstalled() {
        let ids = SignInCatalog.commands(installed: ["crush", "kimi"]).map(\.id)
        #expect(ids == ["crush", "crush.copilot", "crush.openai", "kimi"])
    }

    @Test func geminiSignsInByRedirectWithACodeFallback() throws {
        let gemini = SignInCatalog.all.filter { $0.agentID == "gemini" }
        #expect(gemini.map(\.id) == ["gemini", "gemini.code"])
        #expect(gemini[0].program == "gemini" && gemini[0].args.isEmpty)
        #expect(gemini[1].args.contains("NO_BROWSER=true"))
    }
}
