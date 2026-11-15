import Foundation
import Testing
@testable import TogoKit

struct NativeCommandMetadataTests {
    @Test func liveProgramMetadataPreservesExistingRoutes() throws {
        let wire = #"{"insert":"/model","title":"model","screen":"model","kind":"picker","native":{"opens":"picker","opensWithArgs":"action","args":"optional","state":"provider/model-id","aliases":["models"],"source":"builtin","futureKey":true}}"#
        let row = try JSONDecoder().decode(HarnessCompletion.self, from: Data(wire.utf8))
        #expect(row.screen == "model")
        #expect(row.kind == .picker)
        #expect(row.native?.intent(hasArguments: false) == "picker")
        #expect(row.native?.intent(hasArguments: true) == "action")
        #expect(row.native?.aliases == ["models"])
        let encoded = try JSONEncoder().encode(row)
        #expect(try JSONDecoder().decode(HarnessCompletion.self, from: encoded) == row)
    }

    @Test func oldHelpersStillDecodeWithoutNativeMetadata() throws {
        let wire = #"{"insert":"/model","title":"model","screen":"model","kind":"picker"}"#
        let row = try JSONDecoder().decode(HarnessCompletion.self, from: Data(wire.utf8))
        #expect(row.native == nil)
        #expect(row.screen == "model")
    }

    @Test(arguments: [#"{"opens":"future-workflow","state":42,"subcommands":[{"name":"on"},42,{"name":"off","detail":"Disable"}]}"#, "42", "[]"])
    func unknownOrMalformedOptionalMetadataKeepsTheCatalog(_ metadata: String) throws {
        let wire = "{\"items\":[{\"insert\":\"/test\",\"title\":\"test\",\"native\":" + metadata + "},{\"insert\":\"/model\",\"title\":\"model\"}]}"
        let result = try JSONDecoder().decode(HarnessCompleteResult.self, from: Data(wire.utf8))
        #expect(result.items.map(\.insert) == ["/test", "/model"])
        #expect(result.items[0].native?.state == nil)
        if metadata.contains("future-workflow") {
            #expect(result.items[0].native?.opens == "future-workflow")
            #expect(result.items[0].native?.subcommands?.map(\.name) == ["on", "off"])
        }
    }
}
