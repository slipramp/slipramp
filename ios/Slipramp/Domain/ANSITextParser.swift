struct ANSITextColor: Equatable, Sendable {
    static let defaultBackground = ANSITextColor(red: 16, green: 17, blue: 19)
    static let defaultForeground = ANSITextColor(red: 241, green: 243, blue: 245)

    let red: UInt8
    let green: UInt8
    let blue: UInt8
}

struct ANSITextStyle: Equatable, Sendable {
    var backgroundColor: ANSITextColor?
    var foregroundColor: ANSITextColor?
    var isBold: Bool
    var isDim: Bool
    var isHidden: Bool
    var isItalic: Bool
    var isStrikethrough: Bool
    var isUnderlined: Bool

    init(
        backgroundColor: ANSITextColor? = nil,
        foregroundColor: ANSITextColor? = nil,
        isBold: Bool = false,
        isDim: Bool = false,
        isHidden: Bool = false,
        isItalic: Bool = false,
        isStrikethrough: Bool = false,
        isUnderlined: Bool = false
    ) {
        self.backgroundColor = backgroundColor
        self.foregroundColor = foregroundColor
        self.isBold = isBold
        self.isDim = isDim
        self.isHidden = isHidden
        self.isItalic = isItalic
        self.isStrikethrough = isStrikethrough
        self.isUnderlined = isUnderlined
    }
}

struct ANSITextSegment: Equatable, Sendable {
    let style: ANSITextStyle
    let text: String
}

enum ANSITextParser {
    private enum Color: Equatable {
        case palette(Int)
        case rgb(red: Int, green: Int, blue: Int)
    }

    private struct State {
        var background: Color?
        var foreground: Color?
        var isBold = false
        var isDim = false
        var isHidden = false
        var isInverse = false
        var isItalic = false
        var isStrikethrough = false
        var isUnderlined = false
    }

    private struct ControlSequence {
        let end: Int
        let final: UInt32
        let parameters: String
    }

    private static let palette = [
        ANSITextColor(red: 102, green: 102, blue: 102),
        ANSITextColor(red: 205, green: 49, blue: 49),
        ANSITextColor(red: 13, green: 188, blue: 121),
        ANSITextColor(red: 229, green: 229, blue: 16),
        ANSITextColor(red: 36, green: 114, blue: 200),
        ANSITextColor(red: 188, green: 63, blue: 188),
        ANSITextColor(red: 17, green: 168, blue: 205),
        ANSITextColor(red: 229, green: 229, blue: 229),
        ANSITextColor(red: 133, green: 133, blue: 133),
        ANSITextColor(red: 241, green: 76, blue: 76),
        ANSITextColor(red: 35, green: 209, blue: 139),
        ANSITextColor(red: 245, green: 245, blue: 67),
        ANSITextColor(red: 59, green: 142, blue: 234),
        ANSITextColor(red: 214, green: 112, blue: 214),
        ANSITextColor(red: 41, green: 184, blue: 219),
        ANSITextColor(red: 255, green: 255, blue: 255),
    ]
    private static let colorCubeValues: [UInt8] = [0, 95, 135, 175, 215, 255]

    static func parse(_ input: String) -> [ANSITextSegment] {
        let scalars = Array(input.unicodeScalars)
        var segments: [ANSITextSegment] = []
        var state = State()
        var position = 0
        var textStart = 0

        func appendText(from start: Int, to end: Int) {
            guard start < end else {
                return
            }

            let text = String(String.UnicodeScalarView(scalars[start ..< end]))
            let style = style(for: state)
            if let previous = segments.last, previous.style == style {
                segments[segments.count - 1] = ANSITextSegment(
                    style: style,
                    text: previous.text + text
                )
                return
            }

            segments.append(ANSITextSegment(style: style, text: text))
        }

        while position < scalars.count {
            let value = scalars[position].value
            let isEscape = value == 0x1b
            let isControl =
                (value < 0x20 && value != 0x09 && value != 0x0a && value != 0x0d) ||
                value == 0x7f

            if !isEscape, !isControl, value != 0x9b {
                position += 1
                continue
            }

            appendText(from: textStart, to: position)

            if value == 0x9b {
                let sequence = readControlSequence(scalars, start: position + 1)
                if let sequence, sequence.final == 0x6d {
                    applyGraphicsRendition(to: &state, value: sequence.parameters)
                }
                position = sequence?.end ?? scalars.count
                textStart = position
                continue
            }

            if !isEscape {
                position += 1
                textStart = position
                continue
            }

            let introducer = scalars[safe: position + 1]?.value
            if introducer == 0x5b {
                let sequence = readControlSequence(scalars, start: position + 2)
                if let sequence, sequence.final == 0x6d {
                    applyGraphicsRendition(to: &state, value: sequence.parameters)
                }
                position = sequence?.end ?? scalars.count
            } else if isControlStringIntroducer(introducer) {
                position = readControlStringEnd(scalars, start: position + 2) ?? scalars.count
            } else {
                position = min(position + 2, scalars.count)
            }
            textStart = position
        }

        appendText(from: textStart, to: scalars.count)
        return segments
    }

    private static func readControlSequence(
        _ scalars: [Unicode.Scalar],
        start: Int
    ) -> ControlSequence? {
        guard start < scalars.count else {
            return nil
        }

        for position in start ..< scalars.count {
            let value = scalars[position].value
            if value >= 0x40, value <= 0x7e {
                return ControlSequence(
                    end: position + 1,
                    final: value,
                    parameters: String(String.UnicodeScalarView(scalars[start ..< position]))
                )
            }
        }

        return nil
    }

    private static func readControlStringEnd(
        _ scalars: [Unicode.Scalar],
        start: Int
    ) -> Int? {
        guard start < scalars.count else {
            return nil
        }

        for position in start ..< scalars.count {
            if scalars[position].value == 0x07 {
                return position + 1
            }
            if scalars[position].value == 0x1b,
               scalars[safe: position + 1]?.value == 0x5c {
                return position + 2
            }
        }

        return nil
    }

    private static func isControlStringIntroducer(_ value: UInt32?) -> Bool {
        switch value {
        case 0x5d, 0x50, 0x58, 0x5e, 0x5f:
            true
        default:
            false
        }
    }

    private static func applyGraphicsRendition(to state: inout State, value: String) {
        let rawParameters = value.isEmpty
            ? [Substring("0")]
            : value.split(separator: ";", omittingEmptySubsequences: false)
        var parameters: [Int] = []
        parameters.reserveCapacity(rawParameters.count)

        for rawParameter in rawParameters {
            if rawParameter.isEmpty {
                parameters.append(0)
            } else if let parameter = Int(rawParameter) {
                parameters.append(parameter)
            } else {
                return
            }
        }

        var index = 0
        while index < parameters.count {
            let parameter = parameters[index]
            switch parameter {
            case 0:
                state = State()
            case 1:
                state.isBold = true
            case 2:
                state.isDim = true
            case 3:
                state.isItalic = true
            case 4:
                state.isUnderlined = true
            case 7:
                state.isInverse = true
            case 8:
                state.isHidden = true
            case 9:
                state.isStrikethrough = true
            case 21:
                state.isBold = false
            case 22:
                state.isBold = false
                state.isDim = false
            case 23:
                state.isItalic = false
            case 24:
                state.isUnderlined = false
            case 27:
                state.isInverse = false
            case 28:
                state.isHidden = false
            case 29:
                state.isStrikethrough = false
            case 30 ... 37:
                state.foreground = .palette(parameter - 30)
            case 38, 48:
                if let parsed = readExtendedColor(parameters, start: index + 1) {
                    if parameter == 38 {
                        state.foreground = parsed.color
                    } else {
                        state.background = parsed.color
                    }
                    index = parsed.end
                }
            case 39:
                state.foreground = nil
            case 40 ... 47:
                state.background = .palette(parameter - 40)
            case 49:
                state.background = nil
            case 90 ... 97:
                state.foreground = .palette(parameter - 90 + 8)
            case 100 ... 107:
                state.background = .palette(parameter - 100 + 8)
            default:
                break
            }

            index += 1
        }
    }

    private static func readExtendedColor(
        _ parameters: [Int],
        start: Int
    ) -> (color: Color, end: Int)? {
        guard let mode = parameters[safe: start] else {
            return nil
        }

        if mode == 5 {
            guard let paletteIndex = parameters[safe: start + 1],
                  paletteIndex >= 0, paletteIndex <= 255 else {
                return nil
            }
            return (.palette(paletteIndex), start + 1)
        }

        guard mode == 2,
              let red = parameters[safe: start + 1],
              let green = parameters[safe: start + 2],
              let blue = parameters[safe: start + 3],
              [red, green, blue].allSatisfy({ $0 >= 0 && $0 <= 255 }) else {
            return nil
        }

        return (.rgb(red: red, green: green, blue: blue), start + 3)
    }

    private static func style(for state: State) -> ANSITextStyle {
        var foreground = resolveColor(state.foreground, isBold: state.isBold)
        var background = resolveColor(state.background, isBold: false)

        if state.isInverse {
            (foreground, background) = (
                background ?? .defaultBackground,
                foreground ?? .defaultForeground
            )
        }

        return ANSITextStyle(
            backgroundColor: background,
            foregroundColor: foreground,
            isBold: state.isBold,
            isDim: state.isDim,
            isHidden: state.isHidden,
            isItalic: state.isItalic,
            isStrikethrough: state.isStrikethrough,
            isUnderlined: state.isUnderlined
        )
    }

    private static func resolveColor(_ color: Color?, isBold: Bool) -> ANSITextColor? {
        guard let color else {
            return nil
        }

        switch color {
        case let .rgb(red, green, blue):
            return ANSITextColor(red: UInt8(red), green: UInt8(green), blue: UInt8(blue))
        case let .palette(value):
            var colorIndex = value
            if isBold, colorIndex < 8 {
                colorIndex += 8
            }
            if colorIndex < palette.count {
                return palette[colorIndex]
            }
            if colorIndex < 232 {
                let cubeIndex = colorIndex - 16
                return ANSITextColor(
                    red: colorCubeValues[cubeIndex / 36],
                    green: colorCubeValues[(cubeIndex % 36) / 6],
                    blue: colorCubeValues[cubeIndex % 6]
                )
            }

            let channel = UInt8(8 + (colorIndex - 232) * 10)
            return ANSITextColor(red: channel, green: channel, blue: channel)
        }
    }
}

private extension Collection {
    subscript(safe index: Index) -> Element? {
        indices.contains(index) ? self[index] : nil
    }
}
