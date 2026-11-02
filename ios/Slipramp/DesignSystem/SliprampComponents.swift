import SwiftUI
import UIKit

struct SliprampCollapsibleSection<Content: View>: View {
    private let title: String
    @Binding private var isExpanded: Bool
    private let content: Content

    init(
        _ title: String,
        isExpanded: Binding<Bool>,
        @ViewBuilder content: () -> Content
    ) {
        self.title = title
        _isExpanded = isExpanded
        self.content = content()
    }

    var body: some View {
        Section {
            if isExpanded {
                content
            }
        } header: {
            Button {
                withAnimation(.easeInOut(duration: 0.2)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack {
                    Text(title)
                    Spacer()
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.semibold))
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(isExpanded ? "Expanded" : "Collapsed")
        }
    }
}

struct SliprampScreenBackground: View {
    var decorative = false

    var body: some View {
        ZStack {
            SliprampTheme.canvas

            if decorative {
                Circle()
                    .fill(SliprampTheme.blush.opacity(0.12))
                    .frame(width: 280, height: 280)
                    .blur(radius: 4)
                    .offset(x: 150, y: -310)

                Circle()
                    .fill(SliprampTheme.accent.opacity(0.10))
                    .frame(width: 240, height: 240)
                    .blur(radius: 8)
                    .offset(x: -170, y: 350)
            }
        }
        .ignoresSafeArea()
        .accessibilityHidden(true)
    }
}

struct SliprampCard<Content: View>: View {
    private let content: Content

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        content
            .padding(SliprampTheme.Space.large)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(SliprampTheme.surface, in: RoundedRectangle(cornerRadius: SliprampTheme.Radius.card))
            .overlay {
                RoundedRectangle(cornerRadius: SliprampTheme.Radius.card)
                    .stroke(SliprampTheme.border.opacity(0.7), lineWidth: 1)
            }
            .shadow(color: SliprampTheme.shadow, radius: 12, y: 4)
    }
}

struct SliprampSectionHeader: View {
    let title: String
    var detail: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title)
                .font(.title3.weight(.bold))
            Spacer()
            if let detail {
                Text(detail)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

struct SliprampStatusPill: View {
    let label: String
    let tone: SliprampTone
    var symbol: String?

    var body: some View {
        HStack(spacing: 5) {
            if let symbol {
                Image(systemName: symbol)
                    .imageScale(.small)
            }
            Text(label)
                .lineLimit(1)
        }
        .font(.caption.weight(.semibold))
        .foregroundStyle(tone.color)
        .padding(.horizontal, 9)
        .padding(.vertical, 6)
        .background(tone.fill, in: Capsule())
        .accessibilityElement(children: .combine)
    }
}

struct SliprampMetricCard: View {
    let label: String
    let value: Int
    let symbol: String
    let tone: SliprampTone

    var body: some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
            Image(systemName: symbol)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(tone.color)
                .frame(width: 30, height: 30)
                .background(tone.fill, in: Circle())

            Text(value, format: .number)
                .font(.title2.monospacedDigit().weight(.bold))

            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(SliprampTheme.Space.medium)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(SliprampTheme.surfaceMuted, in: RoundedRectangle(cornerRadius: SliprampTheme.Radius.control))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(value) \(label.lowercased()) runs")
    }
}

struct SliprampCallout: View {
    let message: String
    let tone: SliprampTone
    var symbol = "exclamationmark.triangle.fill"

    var body: some View {
        HStack(alignment: .top, spacing: SliprampTheme.Space.medium) {
            Image(systemName: symbol)
                .foregroundStyle(tone.color)
            Text(message)
                .font(.subheadline)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(SliprampTheme.Space.large)
        .background(tone.fill, in: RoundedRectangle(cornerRadius: SliprampTheme.Radius.control))
        .accessibilityElement(children: .combine)
    }
}

struct SliprampEmptyState: View {
    let title: String
    let message: String
    let symbol: String
    var tone: SliprampTone = .accent
    var showsMascot = false
    var actionTitle: String?
    var action: (() -> Void)?

    var body: some View {
        VStack(spacing: SliprampTheme.Space.large) {
            if showsMascot {
                SliprampMascot()
                    .frame(width: 170, height: 115)
            } else {
                Image(systemName: symbol)
                    .font(.system(size: 30, weight: .semibold))
                    .foregroundStyle(tone.color)
                    .frame(width: 64, height: 64)
                    .background(tone.fill, in: Circle())
            }

            VStack(spacing: SliprampTheme.Space.small) {
                Text(title)
                    .font(.title3.weight(.bold))
                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }

            if let actionTitle, let action {
                Button(actionTitle, action: action)
                    .buttonStyle(.borderedProminent)
                    .tint(SliprampTheme.accent)
            }
        }
        .padding(SliprampTheme.Space.xLarge)
        .frame(maxWidth: 440)
        .accessibilityElement(children: .contain)
    }
}

struct SliprampLoadingState: View {
    let message: String

    var body: some View {
        VStack(spacing: SliprampTheme.Space.large) {
            ProgressView()
                .controlSize(.large)
                .tint(SliprampTheme.accent)
            Text(message)
                .font(.subheadline.weight(.medium))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

struct SliprampField<Content: View>: View {
    let label: String
    let symbol: String
    private let content: Content

    init(label: String, symbol: String, @ViewBuilder content: () -> Content) {
        self.label = label
        self.symbol = symbol
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
            Label(label, systemImage: symbol)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.secondary)

            content
                .padding(.horizontal, 14)
                .frame(minHeight: 50)
                .background(
                    SliprampTheme.surfaceMuted,
                    in: RoundedRectangle(cornerRadius: SliprampTheme.Radius.control)
                )
                .overlay {
                    RoundedRectangle(cornerRadius: SliprampTheme.Radius.control)
                        .stroke(SliprampTheme.border, lineWidth: 1)
                }
        }
    }
}

struct SliprampPrimaryButtonStyle: ButtonStyle {
    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .foregroundStyle(.white.opacity(isEnabled ? 1 : 0.75))
            .frame(maxWidth: .infinity, minHeight: 52)
            .background(
                backgroundColor(isPressed: configuration.isPressed),
                in: RoundedRectangle(cornerRadius: SliprampTheme.Radius.control)
            )
            .scaleEffect(configuration.isPressed && isEnabled ? 0.98 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }

    private func backgroundColor(isPressed: Bool) -> Color {
        guard isEnabled else {
            return SliprampTheme.accent.opacity(0.35)
        }
        return isPressed ? SliprampTheme.accent.opacity(0.8) : SliprampTheme.accent
    }
}

struct SliprampMark: View {
    private static let image = UIImage(named: "slipramp-mark-master")

    var body: some View {
        Group {
            if let image = Self.image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFit()
            } else {
                Image(systemName: "point.3.filled.connected.trianglepath.dotted")
                    .resizable()
                    .scaledToFit()
                    .foregroundStyle(SliprampTheme.accent)
            }
        }
        .accessibilityHidden(true)
    }
}

struct SliprampMascot: View {
    private static let image = UIImage(named: "slipramp-mascot")

    var body: some View {
        Group {
            if let image = Self.image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFit()
            } else {
                SliprampMark()
                    .padding()
            }
        }
        .accessibilityHidden(true)
    }
}
