import Foundation

typealias Inbox = Components.Schemas.Inbox
typealias InboxActivity = Components.Schemas.InboxActivity
typealias InboxItem = Components.Schemas.InboxItem
typealias Promotion = Components.Schemas.Promotion
typealias PromotionDecisionChoice = Components.Schemas.PromotionDecisionUpdate.DecisionPayload
typealias Run = Components.Schemas.Run
typealias RunList = Components.Schemas.RunList
typealias RunReviewDecision = Components.Schemas.RunReviewDecision

extension Components.Schemas.InboxActivity {
    var total: Int {
        queued + starting + running
    }
}

extension Components.Schemas.InboxItem: Identifiable {}

extension Components.Schemas.Run: Identifiable {
    var displayAction: String {
        actionTitle ?? action
    }

    var statusLabel: String {
        (outcome?.rawValue ?? phase.rawValue)
            .replacingOccurrences(of: "-", with: " ")
            .capitalized
    }

    var canDecideReview: Bool {
        phase == .queued && review.state == .pending
    }

    var canCancel: Bool {
        phase != .finished && cancellationRequestedAt == nil
    }
}

extension Components.Schemas.Promotion: Identifiable {
    var statusLabel: String {
        status.rawValue.replacingOccurrences(of: "-", with: " ").capitalized
    }

    var canDecide: Bool {
        status == .pending && decision == nil
    }
}

extension Components.Schemas.GitCommit {
    var shortSHA: String {
        String(sha.prefix(8))
    }
}

extension Components.Schemas.ParameterValue {
    var displayValue: String {
        switch self {
        case let .case1(value):
            value
        case let .case2(value):
            value ? "true" : "false"
        case let .case3(value):
            value.formatted(.number.precision(.fractionLength(0 ... 6)))
        }
    }
}

extension Components.Schemas.RunSource {
    var displayLabel: String {
        switch _type {
        case .manual:
            "Manual"
        case .api:
            "API"
        case .git:
            "Git trigger"
        case .promotion:
            "Promotion"
        }
    }
}

extension Components.Schemas.PromotionSource {
    var displayLabel: String {
        switch self {
        case .kargo:
            "Kargo"
        case .slipramp:
            "Slipramp"
        }
    }
}

extension Components.Schemas.PromotionPolicyMode {
    var displayLabel: String {
        switch self {
        case .automatic:
            "Automatic"
        case .reviewRequired:
            "Review required"
        }
    }
}

struct ArgoDiffDocument: Decodable, Equatable, Sendable {
    let resources: [ArgoDiffResource]
}

struct ArgoDiffResource: Decodable, Equatable, Identifiable, Sendable {
    let group: String?
    let kind: String
    let namespace: String?
    let name: String
    let diff: String
    let versionMetadataOnly: Bool

    var id: String {
        [group, kind, namespace, name]
            .compactMap { $0 }
            .joined(separator: "/")
    }

    private enum CodingKeys: String, CodingKey {
        case group
        case kind
        case namespace
        case name
        case diff
        case versionMetadataOnly
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        group = try container.decodeIfPresent(String.self, forKey: .group)
        kind = try container.decode(String.self, forKey: .kind)
        namespace = try container.decodeIfPresent(String.self, forKey: .namespace)
        name = try container.decode(String.self, forKey: .name)
        diff = try container.decode(String.self, forKey: .diff)
        versionMetadataOnly = try container.decodeIfPresent(Bool.self, forKey: .versionMetadataOnly) ?? false
    }
}
