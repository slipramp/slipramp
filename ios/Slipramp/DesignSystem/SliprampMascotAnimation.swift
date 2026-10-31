import ImageIO
import SwiftUI
import UIKit

struct SliprampMascotActivityView: View {
    let activity: MascotActivity
    let identity: String

    private var pose: MascotPoseDefinition {
        MascotActivityResolver.pose(for: activity.animation, identity: identity)
    }

    var body: some View {
        HStack(alignment: .center, spacing: SliprampTheme.Space.large) {
            SliprampAnimatedMascot(pose: pose, identity: identity)
                .frame(width: 112, height: 104)

            VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
                Text(activity.title)
                    .font(.headline)

                if let description = activity.description, !description.isEmpty {
                    Text(description)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(SliprampTheme.Space.large)
        .background(background, in: RoundedRectangle(cornerRadius: SliprampTheme.Radius.card))
        .overlay {
            RoundedRectangle(cornerRadius: SliprampTheme.Radius.card)
                .stroke(border, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }

    private var tone: SliprampTone {
        switch activity.tone {
        case .success:
            .success
        case .error:
            .danger
        case .neutral:
            .neutral
        case nil:
            .accent
        }
    }

    private var background: Color {
        activity.tone == nil ? SliprampTheme.surfaceMuted : tone.fill
    }

    private var border: Color {
        activity.tone == nil ? SliprampTheme.border : tone.color.opacity(0.28)
    }
}

struct SliprampAnimatedMascot: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    @State private var frameIndex = 0

    let pose: MascotPoseDefinition
    let identity: String

    var body: some View {
        Group {
            if let frames = MascotSpriteLoader.frames(for: pose.name),
                frames.indices.contains(frameIndex)
            {
                Image(uiImage: frames[frameIndex])
                    .resizable()
                    .scaledToFit()
                    .scaleEffect(pose.scale)
            } else {
                SliprampMascot()
                    .padding(SliprampTheme.Space.medium)
            }
        }
        .accessibilityHidden(true)
        .task(id: animationTaskID) {
            await animate()
        }
    }

    private var animationTaskID: String {
        "\(pose.name):\(identity):\(reduceMotion):\(scenePhase)"
    }

    @MainActor
    private func animate() async {
        frameIndex = 0
        guard !reduceMotion, scenePhase == .active else {
            return
        }

        var elapsed = MascotActivityResolver.phaseOffset(for: identity)

        while !Task.isCancelled {
            frameIndex = pose.frameIndex(at: elapsed)
            let delay = pose.timeUntilNextFrame(at: elapsed)

            do {
                try await Task.sleep(for: .seconds(delay))
            } catch {
                return
            }

            elapsed += delay + 0.000_001
        }
    }
}

@MainActor
private enum MascotSpriteLoader {
    private final class CachedFrames: NSObject {
        let images: [UIImage]

        init(images: [UIImage]) {
            self.images = images
        }
    }

    private static let cache: NSCache<NSString, CachedFrames> = {
        let cache = NSCache<NSString, CachedFrames>()
        cache.countLimit = 4
        return cache
    }()

    static func frames(for pose: String) -> [UIImage]? {
        let key = pose as NSString
        if let cached = cache.object(forKey: key) {
            return cached.images
        }

        let resourceName = "slipramp-axolotl-\(pose)"
        guard
            let url = Bundle.main.url(
                forResource: resourceName,
                withExtension: "webp",
                subdirectory: "Mascots"
            ),
            let source = CGImageSourceCreateWithURL(url as CFURL, nil),
            let strip = CGImageSourceCreateImageAtIndex(source, 0, nil),
            strip.width == 1_152,
            strip.height == 288
        else {
            return nil
        }

        let frameWidth = strip.width / 4
        let images = (0 ..< 4).compactMap { index -> UIImage? in
            let frameRect = CGRect(
                x: CGFloat(index * frameWidth),
                y: 0,
                width: CGFloat(frameWidth),
                height: CGFloat(strip.height)
            )
            guard let frame = strip.cropping(to: frameRect) else {
                return nil
            }
            return UIImage(cgImage: frame)
        }

        guard images.count == 4 else {
            return nil
        }

        cache.setObject(CachedFrames(images: images), forKey: key)
        return images
    }
}
