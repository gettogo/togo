// swift-tools-version: 6.2
import PackageDescription

// The mosh client is GPLv3 (with the COPYING.iOS covenant), so it and its C shim live
// in their own package. Blink's protobuf build is a dynamic framework despite its name;
// the app must embed and sign it.
let package = Package(
    name: "TogoMosh",
    platforms: [.iOS(.v26), .macOS(.v26)],
    products: [
        .library(name: "TogoMosh", targets: ["TogoMosh"]),
    ],
    dependencies: [
        .package(path: "../TogoKit"),
    ],
    targets: [
        .binaryTarget(
            name: "mosh",
            url: "https://github.com/blinksh/mosh-apple/releases/download/v1.4.0%2Bblink-18.4.5/mosh.xcframework.zip",
            checksum: "d6dce7664ecce1b15931d6b0b8aaf3b1cacebe390669df960fe47318cb0dda05"
        ),
        .binaryTarget(
            name: "Protobuf_C_",
            url: "https://github.com/blinksh/protobuf-apple/releases/download/v3.21.1/Protobuf_C_-static.xcframework.zip",
            checksum: "a74e23890cf2093047544e18e999f493cf90be42a0ebd1bf5d4c0252d7cf377a"
        ),
        .target(
            name: "MoshShim",
            dependencies: ["mosh", "Protobuf_C_"],
            linkerSettings: [.linkedLibrary("z"), .linkedLibrary("c++")]
        ),
        .target(
            name: "TogoMosh",
            dependencies: ["MoshShim", .product(name: "TogoKit", package: "TogoKit")],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
        .testTarget(
            name: "TogoMoshTests",
            dependencies: ["TogoMosh"],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
    ],
    swiftLanguageModes: [.v6]
)
