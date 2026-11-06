// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "TogoTerminal",
    platforms: [.iOS(.v26)],
    products: [
        .library(name: "TogoTerminal", targets: ["TogoTerminal"]),
    ],
    dependencies: [
        // Exact pin: the app resolves packages inside a gitignored project, so only
        // an exact version keeps device builds reproducible.
        .package(url: "https://github.com/migueldeicaza/SwiftTerm.git", exact: "1.99.2"),
    ],
    targets: [
        .target(
            name: "TogoTerminal",
            dependencies: [.product(name: "SwiftTerm", package: "SwiftTerm")],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
        .testTarget(
            name: "TogoTerminalTests",
            dependencies: ["TogoTerminal"],
            exclude: ["Fixtures/sample.ghostty", "Fixtures/sample.itermcolors", "Fixtures/sample.Xresources"],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
    ],
    swiftLanguageModes: [.v6]
)
