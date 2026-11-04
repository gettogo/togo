// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "TogoKit",
    platforms: [.iOS(.v26), .macOS(.v26)],
    products: [
        .library(name: "TogoKit", targets: ["TogoKit"]),
    ],
    dependencies: [
        // Citadel 0.12.1 and its swift-nio-ssh fork, vendored to add keyboard-interactive auth.
        .package(path: "../../vendor/Citadel"),
    ],
    targets: [
        .target(
            name: "TogoKit",
            dependencies: [.product(name: "Citadel", package: "Citadel")],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
        .testTarget(
            name: "TogoKitTests",
            dependencies: ["TogoKit"],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
    ]
)
