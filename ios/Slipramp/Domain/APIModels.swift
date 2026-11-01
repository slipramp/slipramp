import Foundation

typealias Inbox = Components.Schemas.Inbox
typealias InboxActivity = Components.Schemas.InboxActivity
typealias InboxItem = Components.Schemas.InboxItem
typealias ServerCompatibility = Components.Schemas.ClientCompatibility
typealias Artifact = Components.Schemas.Artifact
typealias ArtifactList = Components.Schemas.ArtifactList
typealias Promotion = Components.Schemas.Promotion
typealias PromotionList = Components.Schemas.PromotionList
typealias PromotionDecisionChoice = Components.Schemas.PromotionDecisionUpdate.DecisionPayload
typealias Run = Components.Schemas.Run
typealias RunList = Components.Schemas.RunList
typealias RunRerunMode = Components.Schemas.RunRerunMode
typealias RunReviewDecision = Components.Schemas.RunReviewDecision
typealias RunArtifactConsumer = Components.Schemas.RunArtifactConsumer
typealias CatalogTarget = Components.Schemas.Target
typealias TargetAction = Components.Schemas.Action
typealias TargetList = Components.Schemas.TargetSummaryList
typealias TargetSummary = Components.Schemas.TargetSummary
typealias ActionParameter = Components.Schemas.Parameter
typealias ActionParameterValue = Components.Schemas.ParameterValue

enum ClientCompatibilityPolicy {
    static let productAPIContract = 1

    static func validate(_ compatibility: ServerCompatibility) throws {
        guard compatibility.productApiContract == productAPIContract else {
            throw SliprampAPIError.incompatibleProductAPIContract(
                server: compatibility.productApiContract
            )
        }
        guard compatibility.authenticationMethods.contains("api-key") else {
            throw SliprampAPIError.apiKeyAuthenticationUnavailable
        }
    }
}

extension Components.Schemas.InboxActivity {
    var total: Int {
        queued + starting + running
    }
}

extension Components.Schemas.InboxItem: Identifiable {}

extension Components.Schemas.Artifact: Identifiable {
    var isDownloadable: Bool {
        state == .sealed && retentionState == .retained
    }

    var statusLabel: String {
        if retentionState != .retained {
            return retentionState.rawValue.capitalized
        }
        return state.rawValue.capitalized
    }
}

extension Components.Schemas.TargetSummary: Identifiable {
    var displayName: String {
        title ?? name
    }
}

extension Components.Schemas.Target {
    var displayName: String {
        definition.metadata.title ?? definition.metadata.name
    }
}

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

enum ActionParameterDecoder {
    static func decode(
        values: [String: String],
        declarations: [String: ActionParameter]
    ) throws -> [String: ActionParameterValue] {
        var parameters: [String: ActionParameterValue] = [:]

        for name in declarations.keys.sorted() {
            guard let declaration = declarations[name] else {
                continue
            }

            let value = values[name] ?? ""
            if value.isEmpty {
                if declaration.required == true {
                    throw ActionParameterValidationError.required(declaration.title ?? name)
                }
                continue
            }

            switch declaration._type {
            case .string:
                parameters[name] = .case1(value)
            case .boolean:
                guard value == "true" || value == "false" else {
                    throw ActionParameterValidationError.invalidBoolean(declaration.title ?? name)
                }
                parameters[name] = .case2(value == "true")
            case .number:
                guard let number = Double(value), number.isFinite else {
                    throw ActionParameterValidationError.invalidNumber(declaration.title ?? name)
                }
                parameters[name] = .case3(number)
            case ._enum:
                guard declaration.values?.contains(value) == true else {
                    throw ActionParameterValidationError.invalidChoice(declaration.title ?? name)
                }
                parameters[name] = .case1(value)
            }
        }

        return parameters
    }
}

enum ActionParameterValidationError: LocalizedError, Equatable {
    case required(String)
    case invalidBoolean(String)
    case invalidNumber(String)
    case invalidChoice(String)

    var errorDescription: String? {
        switch self {
        case let .required(name):
            return "\(name) is required."
        case let .invalidBoolean(name):
            return "\(name) must be true or false."
        case let .invalidNumber(name):
            return "\(name) must be a number."
        case let .invalidChoice(name):
            return "Choose an allowed value for \(name)."
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
        case .mcp:
            "MCP"
        case .git:
            "Git trigger"
        case .promotion:
            "Promotion"
        }
    }
}

extension Components.Schemas.RunRerunMode {
    var displayLabel: String {
        switch self {
        case .exact:
            "Same revision"
        case .latest:
            "Latest revision"
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
