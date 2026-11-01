import XCTest
@testable import SliprampCore

final class ANSITextParserTests: XCTestCase {
    func testPreservesPlainLogOutput() {
        XCTAssertEqual(
            ANSITextParser.parse("terraform output\n"),
            [ANSITextSegment(style: ANSITextStyle(), text: "terraform output\n")]
        )
    }

    func testRendersStandardColorsAndResetsForeground() {
        XCTAssertEqual(
            ANSITextParser.parse("before \u{1b}[31mred\u{1b}[39m after"),
            [
                ANSITextSegment(style: ANSITextStyle(), text: "before "),
                ANSITextSegment(
                    style: ANSITextStyle(
                        foregroundColor: ANSITextColor(red: 205, green: 49, blue: 49)
                    ),
                    text: "red"
                ),
                ANSITextSegment(style: ANSITextStyle(), text: " after"),
            ]
        )
    }

    func testRendersEmphasisAndExtendedColors() {
        XCTAssertEqual(
            ANSITextParser.parse("\u{1b}[1;38;2;12;34;56;48;5;17mstyled"),
            [
                ANSITextSegment(
                    style: ANSITextStyle(
                        backgroundColor: ANSITextColor(red: 0, green: 0, blue: 95),
                        foregroundColor: ANSITextColor(red: 12, green: 34, blue: 56),
                        isBold: true
                    ),
                    text: "styled"
                ),
            ]
        )
    }

    func testRendersInverseAndTextDecorations() {
        XCTAssertEqual(
            ANSITextParser.parse("\u{1b}[3;4;7;9mstyled"),
            [
                ANSITextSegment(
                    style: ANSITextStyle(
                        backgroundColor: .defaultForeground,
                        foregroundColor: .defaultBackground,
                        isItalic: true,
                        isStrikethrough: true,
                        isUnderlined: true
                    ),
                    text: "styled"
                ),
            ]
        )
    }

    func testWaitsForIncompleteSequence() {
        XCTAssertEqual(
            ANSITextParser.parse("ready\u{1b}[31"),
            [ANSITextSegment(style: ANSITextStyle(), text: "ready")]
        )
        XCTAssertEqual(
            ANSITextParser.parse("ready\u{1b}[31mred"),
            [
                ANSITextSegment(style: ANSITextStyle(), text: "ready"),
                ANSITextSegment(
                    style: ANSITextStyle(
                        foregroundColor: ANSITextColor(red: 205, green: 49, blue: 49)
                    ),
                    text: "red"
                ),
            ]
        )
    }

    func testDiscardsNonDocumentTerminalControls() {
        let hyperlink = "\u{1b}]8;;https://example.com\u{07}label\u{1b}]8;;\u{07}"
        let cursorMovement = "\u{1b}[2A"

        XCTAssertEqual(
            ANSITextParser.parse("before \(hyperlink)\(cursorMovement) after"),
            [ANSITextSegment(style: ANSITextStyle(), text: "before label after")]
        )
    }

    func testPreservesUnicodeWhileRemovingControls() {
        XCTAssertEqual(
            ANSITextParser.parse("ready \u{00b7} \u{1b}[32m成功\u{1b}[0m\n"),
            [
                ANSITextSegment(style: ANSITextStyle(), text: "ready \u{00b7} "),
                ANSITextSegment(
                    style: ANSITextStyle(
                        foregroundColor: ANSITextColor(red: 13, green: 188, blue: 121)
                    ),
                    text: "成功"
                ),
                ANSITextSegment(style: ANSITextStyle(), text: "\n"),
            ]
        )
    }
}
