import Testing
@testable import TogoKit

struct DeviceCommentTests {
    @Test func namesTheModelAndFingerprint() {
        #expect(PublicKeyLine.deviceComment(model: "iPad", fingerprint: "SHA256:1a2b3c4dEFGHijkl") == "togo-iPad-1a2b3c4d")
    }

    @Test func skipsPunctuationInTheFingerprint() {
        #expect(PublicKeyLine.deviceComment(model: "iPhone", fingerprint: "SHA256:a+b/c=dEFGH") == "togo-iPhone-abcdEFGH")
    }

    @Test func twoDevicesReadDifferently() {
        let a = PublicKeyLine.deviceComment(model: "iPad", fingerprint: "SHA256:aaaaaaaa11")
        let b = PublicKeyLine.deviceComment(model: "iPhone", fingerprint: "SHA256:bbbbbbbb22")
        #expect(a != b)
    }

    @Test func modelComesFromTheHardwareIdentifier() {
        #expect(PublicKeyLine.model(fromIdentifier: "iPad16,3") == "iPad")
        #expect(PublicKeyLine.model(fromIdentifier: "iPhone17,1") == "iPhone")
        #expect(PublicKeyLine.model(fromIdentifier: "arm64") == "device")
    }
}
