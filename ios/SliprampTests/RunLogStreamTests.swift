import Foundation
import XCTest
@testable import SliprampCore

final class RunLogStreamTests: XCTestCase {
    func testParserDecodesChunkAndTerminalEvent() throws {
        var parser = RunLogSSEParser()

        XCTAssertNil(try parser.consume(line: "id: 3"))
        XCTAssertNil(try parser.consume(line: "event: chunk"))
        XCTAssertNil(
            try parser.consume(
                line: "data: {\"offsetBytes\":0,\"nextOffsetBytes\":3,\"contentsBase64\":\"YWJj\"}"
            )
        )
        XCTAssertEqual(
            try parser.consume(line: ""),
            .chunk(Data("abc".utf8), nextOffset: 3)
        )

        XCTAssertNil(try parser.consume(line: "id: 3"))
        XCTAssertNil(try parser.consume(line: "event: sealed"))
        XCTAssertNil(try parser.consume(line: "data: {}"))
        XCTAssertEqual(try parser.consume(line: ""), .sealed(offset: 3))
    }

    func testParserRemovesReplayOverlapAfterReconnect() throws {
        var parser = RunLogSSEParser(offset: 3)

        XCTAssertNil(try parser.consume(line: "id: 6"))
        XCTAssertNil(try parser.consume(line: "event: chunk"))
        XCTAssertNil(
            try parser.consume(
                line: "data: {\"offsetBytes\":0,\"nextOffsetBytes\":6,\"contentsBase64\":\"YWJjZGVm\"}"
            )
        )
        XCTAssertEqual(
            try parser.consume(line: ""),
            .chunk(Data("def".utf8), nextOffset: 6)
        )
    }

    func testParserRejectsInconsistentChunkOffsets() throws {
        var parser = RunLogSSEParser()

        XCTAssertNil(try parser.consume(line: "id: 5"))
        XCTAssertNil(try parser.consume(line: "event: chunk"))
        XCTAssertNil(
            try parser.consume(
                line: "data: {\"offsetBytes\":0,\"nextOffsetBytes\":5,\"contentsBase64\":\"YWJj\"}"
            )
        )
        XCTAssertThrowsError(try parser.consume(line: "")) { error in
            XCTAssertTrue(error.localizedDescription.contains("inconsistent byte offsets"))
        }
    }

    func testByteParserDispatchesFinalEventFromTrailingBlankLine() throws {
        var parser = RunLogSSEParser()
        let stream = Data(
            "id: 0\nevent: sealed\ndata: {}\n\n".utf8
        )
        var updates: [RunLogStreamUpdate] = []

        for byte in stream {
            if let update = try parser.consume(byte: byte) {
                updates.append(update)
            }
        }

        XCTAssertEqual(updates, [.sealed(offset: 0)])
    }
}
