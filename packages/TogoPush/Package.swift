// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "TogoPush",
    platforms: [.iOS(.v26), .watchOS(.v26), .macOS(.v26)],
    products: [
        .library(name: "TogoPush", targets: ["TogoPush"]),
    ],
    targets: [
        .target(
            name: "TogoPush",
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
        .testTarget(
            name: "TogoPushTests",
            dependencies: ["TogoPush"],
            swiftSettings: [.treatAllWarnings(as: .error)]
        ),
    ]
)
