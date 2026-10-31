import Foundation

enum MascotActivityTone: Equatable, Sendable {
    case success
    case error
    case neutral
}

struct MascotActivity: Equatable, Sendable {
    let animation: MascotAnimation
    let title: String
    let description: String?
    let tone: MascotActivityTone?

    init(
        animation: MascotAnimation,
        title: String,
        description: String? = nil,
        tone: MascotActivityTone? = nil
    ) {
        self.animation = animation
        self.title = title
        self.description = description
        self.tone = tone
    }
}

enum MascotActivityResolver {
    static func activity(for run: Run) -> MascotActivity? {
        if run.phase == .finished {
            return resultActivity(for: run)
        }
        if run.observationState == .unknown {
            return nil
        }

        if run.cancellationRequestedAt != nil {
            return MascotActivity(
                animation: run.phase == .queued ? .waiting : .working,
                title: "Stopping your run",
                description: "Waiting for the action to stop."
            )
        }

        switch run.phase {
        case .queued:
            if run.review.state == .pending {
                return MascotActivity(
                    animation: .waiting,
                    title: "Waiting for approval",
                    description: "This Git-triggered Run needs an operator's approval before assignment."
                )
            }
            return MascotActivity(
                animation: .waiting,
                title: "Waiting for a turn",
                description: "Your Run is queued and waiting for a Controller."
            )
        case .assigned:
            return MascotActivity(
                animation: .working,
                title: "Getting your Run ready",
                description: "The Controller has picked up your Run."
            )
        case .preparing:
            return MascotActivity(
                animation: .working,
                title: "Hard hat on. Getting set!",
                description: "Preparing the execution environment for your Action."
            )
        case .running:
            return MascotActivity(
                animation: .working,
                title: "A little axolotl. A lot of work.",
                description: "Your Action is running. Follow along in the live log."
            )
        case .finished:
            return nil
        }
    }

    static func activity(for promotion: Promotion) -> MascotActivity {
        switch promotion.status {
        case .creating:
            return MascotActivity(
                animation: .waiting,
                title: "Making a little momentum",
                description: "Creating your Promotion in Kargo and waiting for its data."
            )
        case .preparing:
            return MascotActivity(
                animation: .working,
                title: "Good things are in motion",
                description: "Preparing the Promotion and its diffs."
            )
        case .pending:
            return MascotActivity(
                animation: .waiting,
                title: "Ready when you are",
                description: "Review the prepared changes, then approve or reject this Promotion."
            )
        case .approved:
            return MascotActivity(
                animation: .celebrating,
                title: "Promotion approved",
                description: "Kargo can continue with the approved candidate.",
                tone: .success
            )
        case .rejected:
            return MascotActivity(
                animation: .frustrated,
                title: "Promotion rejected",
                description: "The candidate was not approved for this Stage.",
                tone: .error
            )
        case .expired:
            return MascotActivity(
                animation: .phone,
                title: "Review window expired",
                description: promotion.message ?? "No decision was recorded before the review window closed.",
                tone: .neutral
            )
        case .error:
            return MascotActivity(
                animation: .frustrated,
                title: "Promotion failed",
                description: promotion.message ?? "The Promotion could not be created or prepared.",
                tone: .error
            )
        }
    }

    static func pose(for animation: MascotAnimation, identity: String) -> MascotPoseDefinition {
        let poses = MascotCatalog.poses(for: animation)
        precondition(!poses.isEmpty, "Mascot animation pools cannot be empty")

        let hash = animationHash("\(animation.rawValue):\(identity)")
        return poses[Int(hash % UInt32(poses.count))]
    }

    static func phaseOffset(for identity: String) -> TimeInterval {
        TimeInterval(animationHash(identity) % 10_000) / 1_000
    }

    static func animationHash(_ value: String) -> UInt32 {
        value.utf16.reduce(2_166_136_261) { hash, codeUnit in
            (hash ^ UInt32(codeUnit)) &* 16_777_619
        }
    }

    private static func resultActivity(for run: Run) -> MascotActivity? {
        switch run.outcome {
        case .some(.succeeded):
            return MascotActivity(
                animation: .celebrating,
                title: "Run succeeded",
                tone: .success
            )
        case .some(.failed):
            return MascotActivity(
                animation: .frustrated,
                title: "Run failed",
                description: run.outcomeMessage ?? "The Action failed. Details are in the log.",
                tone: .error
            )
        case .some(.timedOut):
            return MascotActivity(
                animation: .frustrated,
                title: "Run timed out",
                description: run.outcomeMessage ?? "The Action exceeded its time limit.",
                tone: .error
            )
        case .some(.canceled):
            return MascotActivity(
                animation: .phone,
                title: "Run canceled",
                description: run.outcomeMessage ?? "Your Run was canceled.",
                tone: .neutral
            )
        case nil:
            return nil
        }
    }
}

extension MascotPoseDefinition {
    func frameIndex(at elapsed: TimeInterval) -> Int {
        let progress = cycleProgress(at: elapsed)
        return frameStarts.lastIndex(where: { $0 <= progress }) ?? 0
    }

    func timeUntilNextFrame(at elapsed: TimeInterval) -> TimeInterval {
        let progress = cycleProgress(at: elapsed)
        let nextStart = frameStarts.first(where: { $0 > progress }) ?? 1
        return max((nextStart - progress) * duration, 0.01)
    }

    private func cycleProgress(at elapsed: TimeInterval) -> Double {
        let cycleTime = elapsed.truncatingRemainder(dividingBy: duration)
        return (cycleTime >= 0 ? cycleTime : cycleTime + duration) / duration
    }
}
