// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "GoModelBar",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "GoModelBar", targets: ["GoModelBar"])],
    dependencies: [.package(url: "https://github.com/jpsim/Yams.git", exact: "6.2.2")],
    targets: [
        .target(name: "GoModelCore", dependencies: ["Yams"]),
        .executableTarget(name: "GoModelBar", dependencies: ["GoModelCore"]),
        .testTarget(name: "GoModelCoreTests", dependencies: ["GoModelCore"]),
        .testTarget(name: "GoModelBarTests", dependencies: ["GoModelBar"]),
    ]
)
