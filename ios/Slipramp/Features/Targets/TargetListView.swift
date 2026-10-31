import SwiftUI

@MainActor
private final class TargetListViewModel: ObservableObject {
    @Published private(set) var targets: [TargetSummary] = []
    @Published private(set) var total: Int64 = 0
    @Published private(set) var isLoading = false
    @Published var error: String?

    private let client: SliprampAPIClient
    private var requestGeneration = 0

    init(client: SliprampAPIClient) {
        self.client = client
    }

    func load(query: String) async {
        requestGeneration += 1
        let generation = requestGeneration
        isLoading = true

        defer {
            if generation == requestGeneration {
                isLoading = false
            }
        }

        let normalizedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        let requestQuery = normalizedQuery.isEmpty ? nil : String(normalizedQuery.prefix(200))

        do {
            let response = try await client.listTargets(query: requestQuery)
            guard generation == requestGeneration, !Task.isCancelled else {
                return
            }

            targets = response.items
            total = response.total
            error = nil
        } catch {
            guard generation == requestGeneration, !Task.isCancelled else {
                return
            }
            self.error = error.localizedDescription
        }
    }
}

struct TargetListView: View {
    @StateObject private var model: TargetListViewModel
    @State private var searchText = ""

    private let client: SliprampAPIClient
    private let onRunChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        onRunChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.onRunChanged = onRunChanged
        _model = StateObject(wrappedValue: TargetListViewModel(client: client))
    }

    var body: some View {
        ZStack {
            SliprampScreenBackground()

            Group {
                if !model.targets.isEmpty {
                    ScrollView {
                        LazyVStack(spacing: SliprampTheme.Space.medium) {
                            ForEach(model.targets) { target in
                                NavigationLink {
                                    TargetDetailView(
                                        client: client,
                                        targetID: target.id,
                                        initialSummary: target,
                                        onRunChanged: onRunChanged
                                    )
                                } label: {
                                    TargetSummaryCard(target: target)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(SliprampTheme.Space.large)
                        .padding(.bottom, SliprampTheme.Space.xLarge)
                    }
                    .refreshable {
                        await model.load(query: searchText)
                    }
                } else if model.isLoading {
                    SliprampLoadingState(message: "Loading Targets…")
                } else if let error = model.error {
                    SliprampEmptyState(
                        title: "Targets unavailable",
                        message: error,
                        symbol: "exclamationmark.triangle.fill",
                        tone: .danger,
                        actionTitle: "Try again"
                    ) {
                        Task { await model.load(query: searchText) }
                    }
                } else if searchText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    SliprampEmptyState(
                        title: "No Targets",
                        message: "Active catalog Targets will appear here after a Repository is refreshed.",
                        symbol: "scope",
                        tone: .accent
                    )
                } else {
                    SliprampEmptyState(
                        title: "No matching Targets",
                        message: "Try a Target, Repository, or Action name.",
                        symbol: "magnifyingglass",
                        tone: .neutral
                    )
                }
            }
        }
        .navigationTitle("Targets")
        .searchable(text: $searchText, prompt: "Target, Repository, or Action")
        .toolbar {
            if model.total > Int64(model.targets.count) {
                ToolbarItem(placement: .topBarTrailing) {
                    Text("\(model.targets.count) of \(model.total)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .task(id: searchText) {
            if !searchText.isEmpty {
                do {
                    try await Task.sleep(for: .milliseconds(250))
                } catch {
                    return
                }
            }
            await model.load(query: searchText)
        }
    }
}

private struct TargetSummaryCard: View {
    let target: TargetSummary

    var body: some View {
        SliprampCard {
            VStack(alignment: .leading, spacing: SliprampTheme.Space.medium) {
                HStack(alignment: .top, spacing: SliprampTheme.Space.medium) {
                    VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
                        Text(target.displayName)
                            .font(.headline)
                            .foregroundStyle(.primary)

                        if target.title != nil {
                            Text(target.name)
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                        }
                    }

                    Spacer(minLength: SliprampTheme.Space.small)

                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.tertiary)
                }

                Text(target.repositoryName)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                HStack(spacing: SliprampTheme.Space.small) {
                    Label(
                        "\(target.actions.count) action\(target.actions.count == 1 ? "" : "s")",
                        systemImage: "play.square.stack"
                    )
                    .font(.caption)
                    .foregroundStyle(.secondary)

                    Spacer()

                    Text(target.desiredCommit.shortSHA)
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                }

                if target.actions.contains("sync") {
                    let status = reconciliationPresentation(target.reconciliation.comparison)
                    SliprampStatusPill(
                        label: status.label,
                        tone: status.tone,
                        symbol: status.symbol
                    )
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityHint("Opens Target details and Actions")
    }

    private func reconciliationPresentation(
        _ comparison: Components.Schemas.TargetReconciliationStatus.ComparisonPayload
    ) -> (label: String, tone: SliprampTone, symbol: String) {
        switch comparison {
        case .matched:
            ("Sync matched", .success, "checkmark.circle.fill")
        case .different:
            ("Sync differs", .warning, "arrow.triangle.2.circlepath")
        case .unknown:
            ("Sync unknown", .neutral, "questionmark.circle")
        }
    }
}
