import SwiftUI

@MainActor
private final class TargetDetailViewModel: ObservableObject {
    @Published private(set) var target: CatalogTarget?
    @Published private(set) var isLoading = false
    @Published var error: String?

    private let client: SliprampAPIClient
    private let targetID: String

    init(client: SliprampAPIClient, targetID: String) {
        self.client = client
        self.targetID = targetID
    }

    func load() async {
        guard !isLoading else {
            return
        }

        isLoading = true
        defer { isLoading = false }

        do {
            target = try await client.getTarget(id: targetID)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct TargetDetailView: View {
    @StateObject private var model: TargetDetailViewModel

    private let client: SliprampAPIClient
    private let initialSummary: TargetSummary?
    private let initialDisplayName: String?
    private let onRunChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        targetID: String,
        initialSummary: TargetSummary? = nil,
        initialDisplayName: String? = nil,
        onRunChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.initialSummary = initialSummary
        self.initialDisplayName = initialDisplayName
        self.onRunChanged = onRunChanged
        _model = StateObject(wrappedValue: TargetDetailViewModel(client: client, targetID: targetID))
    }

    var body: some View {
        Group {
            if let target = model.target {
                List {
                    overviewSection(target)
                    reconciliationSection(target)
                    actionsSection(target)
                    defaultsSection(target)
                }
                .scrollContentBackground(.hidden)
                .background(SliprampTheme.canvas)
                .refreshable {
                    await model.load()
                }
            } else if model.isLoading {
                SliprampLoadingState(message: "Loading Target…")
            } else {
                ContentUnavailableView {
                    Label("Target unavailable", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(model.error ?? "Slipramp could not load this Target.")
                } actions: {
                    Button("Try again") {
                        Task { await model.load() }
                    }
                }
            }
        }
        .navigationTitle(
            model.target?.displayName
                ?? initialSummary?.displayName
                ?? initialDisplayName
                ?? "Target"
        )
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await model.load()
        }
    }

    @ViewBuilder
    private func overviewSection(_ target: CatalogTarget) -> some View {
        Section("Target") {
            if target.archivedAt != nil {
                SliprampCallout(
                    message: "This Target is archived. Its definition remains available for history, "
                        + "but it cannot start new Runs.",
                    tone: .neutral,
                    symbol: "archivebox.fill"
                )
            }

            if target.definition.metadata.title != nil {
                LabeledContent("Name") {
                    Text(target.definition.metadata.name)
                        .fontDesign(.monospaced)
                }
            }
            LabeledContent("Repository", value: target.repositoryName)
            LabeledContent("Desired commit") {
                Text(target.definition.status.desiredCommit.shortSHA)
                    .fontDesign(.monospaced)
            }
            if let title = target.definition.status.desiredCommit.title {
                Text(title)
                    .foregroundStyle(.secondary)
            }
            LabeledContent("Catalog commit") {
                Text(target.catalogCommit.shortSHA)
                    .fontDesign(.monospaced)
            }
        }
    }

    @ViewBuilder
    private func reconciliationSection(_ target: CatalogTarget) -> some View {
        if target.definition.spec.actions.additionalProperties["sync"] != nil {
            let reconciliation = target.definition.status.reconciliation

            Section("Sync reconciliation") {
                LabeledContent("Comparison") {
                    let presentation = reconciliationPresentation(
                        reconciliation.comparison,
                        archived: target.archivedAt != nil
                    )
                    SliprampStatusPill(
                        label: presentation.label,
                        tone: presentation.tone,
                        symbol: presentation.symbol
                    )
                }

                if let checkpoint = reconciliation.lastAttemptedSync {
                    NavigationLink {
                        RunDetailView(
                            client: client,
                            runID: checkpoint.runId,
                            onRunChanged: onRunChanged
                        )
                    } label: {
                        VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
                            Text("Last attempt")
                            Text(checkpointLabel(checkpoint))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                if let checkpoint = reconciliation.lastSuccessfulSync {
                    NavigationLink {
                        RunDetailView(
                            client: client,
                            runID: checkpoint.runId,
                            onRunChanged: onRunChanged
                        )
                    } label: {
                        VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
                            Text("Last success")
                            Text(checkpointLabel(checkpoint))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }

    private func actionsSection(_ target: CatalogTarget) -> some View {
        let actions = target.definition.spec.actions.additionalProperties

        return Section("Actions") {
            ForEach(actions.keys.sorted(), id: \.self) { name in
                if let action = actions[name] {
                    NavigationLink {
                        ActionDetailView(
                            client: client,
                            target: target,
                            actionName: name,
                            action: action,
                            onRunChanged: onRunChanged
                        )
                    } label: {
                        ActionRow(name: name, action: action)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func defaultsSection(_ target: CatalogTarget) -> some View {
        let spec = target.definition.spec
        if spec.runtime != nil || spec.placement != nil || spec.timeout != nil || spec.path != nil {
            Section("Defaults") {
                if let runtime = spec.runtime {
                    LabeledContent("Runtime", value: runtime)
                }
                if let placement = spec.placement {
                    LabeledContent("Placement", value: placement)
                }
                if let timeout = spec.timeout {
                    LabeledContent("Timeout", value: timeout)
                }
                if let path = spec.path {
                    LabeledContent("Path") {
                        Text(path)
                            .fontDesign(.monospaced)
                    }
                }
            }
        }
    }

    private func reconciliationPresentation(
        _ comparison: Components.Schemas.TargetReconciliationStatus.ComparisonPayload,
        archived: Bool
    ) -> (label: String, tone: SliprampTone, symbol: String) {
        if archived {
            return ("Unknown", .neutral, "archivebox.fill")
        }

        switch comparison {
        case .matched:
            return ("Matched", .success, "checkmark.circle.fill")
        case .different:
            return ("Different", .warning, "arrow.triangle.2.circlepath")
        case .unknown:
            return ("Unknown", .neutral, "questionmark.circle")
        }
    }

    private func checkpointLabel(_ checkpoint: Components.Schemas.TargetSyncRunCheckpoint) -> String {
        let result = checkpoint.outcome?.rawValue ?? checkpoint.phase.rawValue
        return "\(result.replacingOccurrences(of: "-", with: " ").capitalized) · Run \(checkpoint.runId.prefix(8))"
    }
}

private struct ActionRow: View {
    let name: String
    let action: TargetAction

    var body: some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
            HStack(alignment: .firstTextBaseline) {
                Text(action.title ?? name)
                    .font(.headline)

                Spacer()

                if let intent = action.intent {
                    Text(intent.replacingOccurrences(of: "-", with: " ").capitalized)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                }
            }

            if action.title != nil {
                Text(name)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }

            if let description = action.description {
                Text(description)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            let parameterCount = action.parameters?.additionalProperties.count ?? 0
            let inputCount = action.inputs?.additionalProperties.count ?? 0
            if parameterCount > 0 || inputCount > 0 {
                HStack(spacing: SliprampTheme.Space.medium) {
                    if parameterCount > 0 {
                        Label(
                            "\(parameterCount) parameter\(parameterCount == 1 ? "" : "s")",
                            systemImage: "slider.horizontal.3"
                        )
                    }
                    if inputCount > 0 {
                        Label("Artifact input", systemImage: "shippingbox")
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, SliprampTheme.Space.xSmall)
    }
}
