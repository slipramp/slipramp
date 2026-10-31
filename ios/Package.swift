// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "SliprampMobile",
    platforms: [
        .iOS(.v17),
        .macOS(.v13),
    ],
    products: [
        .library(name: "SliprampCore", targets: ["SliprampCore"]),
    ],
    dependencies: [
        .package(
            url: "https://github.com/apple/swift-openapi-generator",
            exact: "1.13.0"
        ),
        .package(
            url: "https://github.com/apple/swift-openapi-runtime",
            exact: "1.12.0"
        ),
        .package(
            url: "https://github.com/apple/swift-openapi-urlsession",
            exact: "1.3.1"
        ),
    ],
    targets: [
        .target(
            name: "SliprampCore",
            dependencies: [
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "OpenAPIURLSession", package: "swift-openapi-urlsession"),
            ],
            path: "Slipramp",
            exclude: [
                "App",
                "DesignSystem",
                "Features",
                "Resources",
            ],
            resources: [
                .copy("openapi-generator-config.yaml"),
                .copy("openapi.yaml"),
            ]
        ),
        .testTarget(
            name: "SliprampCoreTests",
            dependencies: ["SliprampCore"],
            path: "SliprampTests"
        ),
    ]
)
