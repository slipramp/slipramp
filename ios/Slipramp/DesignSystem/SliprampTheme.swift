import SwiftUI
import UIKit

enum SliprampAppearance: String, CaseIterable, Identifiable {
    static let storageKey = "slipramp.appearance"

    case system
    case light
    case dark

    var id: Self { self }

    var label: String {
        switch self {
        case .system:
            "System"
        case .light:
            "Light"
        case .dark:
            "Dark"
        }
    }

    var colorScheme: ColorScheme? {
        switch self {
        case .system:
            nil
        case .light:
            .light
        case .dark:
            .dark
        }
    }
}

enum SliprampTheme {
    enum Space {
        static let xSmall: CGFloat = 4
        static let small: CGFloat = 8
        static let medium: CGFloat = 12
        static let large: CGFloat = 16
        static let xLarge: CGFloat = 24
        static let xxLarge: CGFloat = 32
    }

    enum Radius {
        static let control: CGFloat = 12
        static let card: CGFloat = 18
        static let hero: CGFloat = 26
    }

    static let accent = Color(red: 0.43, green: 0.29, blue: 0.94)
    static let accentSoft = Color(red: 0.64, green: 0.54, blue: 0.96)
    static let blush = Color(red: 0.97, green: 0.56, blue: 0.70)
    static let sun = Color(red: 0.96, green: 0.74, blue: 0.27)

    static let canvas = adaptiveColor(
        light: UIColor(red: 0.97, green: 0.96, blue: 0.99, alpha: 1),
        dark: UIColor(red: 0.07, green: 0.06, blue: 0.10, alpha: 1)
    )
    static let surface = adaptiveColor(
        light: .white,
        dark: UIColor(red: 0.12, green: 0.10, blue: 0.16, alpha: 1)
    )
    static let surfaceMuted = adaptiveColor(
        light: UIColor(red: 0.94, green: 0.92, blue: 0.98, alpha: 1),
        dark: UIColor(red: 0.17, green: 0.14, blue: 0.22, alpha: 1)
    )
    static let border = adaptiveColor(
        light: UIColor(red: 0.86, green: 0.83, blue: 0.91, alpha: 1),
        dark: UIColor(red: 0.28, green: 0.23, blue: 0.35, alpha: 1)
    )
    static let logBackground = Color(red: 0.045, green: 0.05, blue: 0.065)
    static let shadow = Color.black.opacity(0.08)

    private static func adaptiveColor(light: UIColor, dark: UIColor) -> Color {
        Color(uiColor: UIColor { traits in
            traits.userInterfaceStyle == .dark ? dark : light
        })
    }
}

enum SliprampTone {
    case accent
    case info
    case success
    case warning
    case danger
    case neutral

    var color: Color {
        switch self {
        case .accent:
            SliprampTheme.accent
        case .info:
            Color(red: 0.22, green: 0.49, blue: 0.91)
        case .success:
            Color(red: 0.12, green: 0.58, blue: 0.39)
        case .warning:
            Color(red: 0.82, green: 0.48, blue: 0.08)
        case .danger:
            Color(red: 0.86, green: 0.25, blue: 0.37)
        case .neutral:
            .secondary
        }
    }

    var fill: Color {
        color.opacity(0.12)
    }
}
