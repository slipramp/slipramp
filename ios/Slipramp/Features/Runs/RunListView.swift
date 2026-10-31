import SwiftUI

@MainActor
private final class RunListViewModel: ObservableObject {
    @Published private(set) var runs: [Run] = []
    @Published private(set) var total = 0
    @Published private(set) var isLoading = false
    @Published var error: String?

    private let client: SliprampAPIClient

    init(client: SliprampAPIClient) {
        self.client = client
    }

    func load() async {
        guard !isLoading else {
            return
        }
        isLoading = true
        defer { isLoading = false }

        do {
            let response = try await client.listRuns()
            runs = response.items
            total = response.total
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct RunListView: View {
    @StateObject private var model: RunListViewModel
    private let client: SliprampAPIClient
    private let refreshRevision: Int
    private let onRunChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        refreshRevision: Int = 0,
        onRunChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.refreshRevision = refreshRevision
        self.onRunChanged = onRunChanged
        _model = StateObject(wrappedValue: RunListViewModel(client: client))
    }

    var body: some View {
        ZStack {
            SliprampScreenBackground()

            Group {
                if !model.runs.isEmpty {
                    ScrollView {
                        LazyVStack(spacing: SliprampTheme.Space.medium) {
                            ForEach(model.runs) { run in
                                NavigationLink {
                                    RunDetailView(
                                        client: client,
                                        runID: run.id,
                                        initialRun: run,
                                        onRunChanged: onRunChanged
                                    )
                                } label: {
                                    RunRow(run: run)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(SliprampTheme.Space.large)
                        .padding(.bottom, SliprampTheme.Space.xLarge)
                    }
                    .refreshable {
                        await model.load()
                    }
                } else if model.isLoading {
                    SliprampLoadingState(message: "Loading Runs…")
                } else if let error = model.error {
                    SliprampEmptyState(
                        title: "Runs unavailable",
                        message: error,
                        symbol: "exclamationmark.triangle.fill",
                        tone: .danger,
                        actionTitle: "Try again"
                    ) {
                        Task { await model.load() }
                    }
                } else {
                    SliprampEmptyState(
                        title: "No Runs yet",
                        message: "Runs will appear here after an Action is started.",
                        symbol: "clock.arrow.circlepath",
                        tone: .accent
                    )
                }
            }
        }
        .navigationTitle("Runs")
        .toolbar {
            if model.total > model.runs.count {
                ToolbarItem(placement: .topBarTrailing) {
                    Text("\(model.runs.count) of \(model.total)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .task(id: refreshRevision) {
            await model.load()
        }
    }
}

struct RunRow: View {
    let run: Run

    var body: some View {
        SliprampCard {
            HStack(alignment: .top, spacing: SliprampTheme.Space.medium) {
                Image(systemName: statusSymbol)
                    .foregroundStyle(statusTone.color)
                    .font(.headline)
                    .frame(width: 40, height: 40)
                    .background(statusTone.fill, in: Circle())

                VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(run.displayAction)
                            .font(.headline)
                        Spacer(minLength: SliprampTheme.Space.small)
                        Text(run.createdAt, style: .relative)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize()
                    }

                    Text(run.target)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)

                    HStack(spacing: SliprampTheme.Space.small) {
                        SliprampStatusPill(
                            label: run.statusLabel,
                            tone: statusTone,
                            symbol: statusSymbol
                        )

                        Text(run.commit.shortSHA)
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                    }
                }

                Image(systemName: "chevron.right")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.tertiary)
                    .padding(.top, 13)
            }
        }
    }

    private var statusSymbol: String {
        switch run.outcome {
        case .some(.succeeded): "checkmark.circle.fill"
        case .some(.failed), .some(.timedOut): "xmark.octagon.fill"
        case .some(.canceled): "minus.circle.fill"
        default:
            switch run.phase {
            case .running: "play.circle.fill"
            case .preparing, .assigned: "arrow.triangle.2.circlepath.circle.fill"
            case .queued, .finished: "clock.fill"
            }
        }
    }

    private var statusTone: SliprampTone {
        switch run.outcome {
        case .some(.succeeded): .success
        case .some(.failed), .some(.timedOut): .danger
        case .some(.canceled): .neutral
        default:
            switch run.phase {
            case .running: .accent
            case .preparing, .assigned: .info
            case .queued, .finished: .warning
            }
        }
    }
}
