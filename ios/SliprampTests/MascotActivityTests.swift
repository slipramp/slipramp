import XCTest
@testable import SliprampCore

final class MascotActivityTests: XCTestCase {
    func testNativePoseSelectionMatchesTheWebCatalog() {
        let identity = "19d265a2-c116-4e8f-b547-a824a9039574"

        XCTAssertEqual(
            MascotActivityResolver.pose(for: .waiting, identity: identity).name,
            "beach-ball-roll"
        )
        XCTAssertEqual(
            MascotActivityResolver.pose(for: .working, identity: identity).name,
            "flight-controller"
        )
        XCTAssertEqual(
            MascotActivityResolver.pose(for: .celebrating, identity: identity).name,
            "jackpot-stars"
        )
        XCTAssertEqual(
            MascotActivityResolver.pose(for: .frustrated, identity: identity).name,
            "server-bite"
        )
        XCTAssertEqual(
            MascotActivityResolver.pose(for: .phone, identity: identity).name,
            "phone-scroll"
        )
        XCTAssertEqual(MascotActivityResolver.phaseOffset(for: identity), 2.598)
    }

    func testGeneratedCatalogCoversEveryProductionPose() {
        let names = Set(
            MascotAnimation.allCases.flatMap { animation in
                MascotCatalog.poses(for: animation).map(\.name)
            }
        )

        XCTAssertEqual(names.count, MascotCatalog.poseCount)
        XCTAssertEqual(MascotCatalog.poseCount, 118)
        XCTAssertTrue(MascotAnimation.allCases.allSatisfy {
            !MascotCatalog.poses(for: $0).isEmpty
        })
    }

    func testPoseTimingRetainsReviewedFrameHolds() throws {
        let pose = try XCTUnwrap(
            MascotCatalog.poses(for: .celebrating).first(where: { $0.name == "server-hug" })
        )

        XCTAssertEqual(pose.frameIndex(at: 0), 0)
        XCTAssertEqual(pose.frameIndex(at: pose.duration * (pose.frameStarts[1] - 0.001)), 0)
        XCTAssertEqual(pose.frameIndex(at: pose.duration * pose.frameStarts[1]), 1)
        XCTAssertEqual(pose.frameIndex(at: pose.duration * pose.frameStarts[2]), 2)
        XCTAssertEqual(pose.frameIndex(at: pose.duration * pose.frameStarts[3]), 3)
        XCTAssertEqual(pose.frameIndex(at: pose.duration), 0)
    }
}
