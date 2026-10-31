import SwiftUI

@MainActor
private final class InboxViewModel: ObservableObject {
    @Published private(set) var inbox: Inbox?
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
            inbox = try await client.getInbox()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct InboxView: View {
    @StateObject private var model: InboxViewModel
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
        _model = StateObject(wrappedValue: InboxViewModel(client: client))
    }

    var body: some View {
        ZStack {
            SliprampScreenBackground()

            Group {
                if let inbox = model.inbox {
                    inboxContent(inbox)
                } else if model.isLoading {
                    SliprampLoadingState(message: "Loading Inbox…")
                } else {
                    SliprampEmptyState(
                        title: "Inbox unavailable",
                        message: model.error ?? "Slipramp could not load the Inbox.",
                        symbol: "exclamationmark.triangle.fill",
                        tone: .danger,
                        actionTitle: "Try again"
                    ) {
                        Task { await model.load() }
                    }
                }
            }
        }
        .navigationTitle("Inbox")
        .task(id: refreshRevision) {
            await model.load()
        }
    }

    private func inboxContent(_ inbox: Inbox) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: SliprampTheme.Space.xLarge) {
                ActivitySummary(activity: inbox.activity)

                if !inbox.decisions.isEmpty {
                    InboxSection(
                        title: "Decisions",
                        count: inbox.decisions.count,
                        items: inbox.decisions,
                        tone: .accent,
                        client: client,
                        onRunChanged: onRunChanged
                    )
                }

                if !inbox.problems.isEmpty {
                    InboxSection(
                        title: "Problems",
                        count: inbox.problems.count,
                        items: inbox.problems,
                        tone: .warning,
                        client: client,
                        onRunChanged: onRunChanged
                    )
                }

                if inbox.decisions.isEmpty && inbox.problems.isEmpty {
                    SliprampCard {
                        SliprampEmptyState(
                            title: "Inbox clear",
                            message: "There are no pending decisions or operational problems.",
                            symbol: "checkmark.circle.fill",
                            tone: .success,
                            showsMascot: true
                        )
                        .frame(maxWidth: .infinity)
                    }
                }
            }
            .padding(SliprampTheme.Space.large)
            .padding(.bottom, SliprampTheme.Space.xLarge)
        }
        .refreshable {
            await model.load()
        }
    }
}

private struct ActivitySummary: View {
    let activity: InboxActivity

    var body: some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.medium) {
            SliprampSectionHeader(
                title: "Run activity",
                detail: activity.total == 1 ? "1 active" : "\(activity.total) active"
            )

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 92), spacing: SliprampTheme.Space.small)],
                spacing: SliprampTheme.Space.small
            ) {
                SliprampMetricCard(
                    label: "Queued",
                    value: activity.queued,
                    symbol: "clock.fill",
                    tone: .warning
                )
                SliprampMetricCard(
                    label: "Starting",
                    value: activity.starting,
                    symbol: "arrow.triangle.2.circlepath",
                    tone: .info
                )
                SliprampMetricCard(
                    label: "Running",
                    value: activity.running,
                    symbol: "play.fill",
                    tone: .accent
                )
            }
        }
    }
}

private struct InboxSection: View {
    let title: String
    let count: Int
    let items: [InboxItem]
    let tone: SliprampTone
    let client: SliprampAPIClient
    let onRunChanged: @MainActor () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.medium) {
            SliprampSectionHeader(title: title, detail: "\(count)")

            ForEach(items) { item in
                InboxItemLink(
                    item: item,
                    tone: tone,
                    client: client,
                    onRunChanged: onRunChanged
                )
            }
        }
    }
}

private struct InboxItemLink: View {
    let item: InboxItem
    let tone: SliprampTone
    let client: SliprampAPIClient
    let onRunChanged: @MainActor () -> Void

    var body: some View {
        switch item.destination._type {
        case .run:
            NavigationLink {
                RunDetailView(
                    client: client,
                    runID: item.destination.id,
                    onRunChanged: onRunChanged
                )
            } label: {
                InboxItemRow(item: item, tone: tone, showsDisclosure: true)
            }
            .buttonStyle(.plain)
        case .promotion:
            NavigationLink {
                PromotionDetailView(
                    client: client,
                    promotionID: item.destination.id,
                    onPromotionChanged: onRunChanged
                )
            } label: {
                InboxItemRow(item: item, tone: tone, showsDisclosure: true)
            }
            .buttonStyle(.plain)
        case .repository, .cluster:
            InboxItemRow(item: item, tone: tone, showsDisclosure: false)
        }
    }
}

private struct InboxItemRow: View {
    let item: InboxItem
    let tone: SliprampTone
    let showsDisclosure: Bool

    var body: some View {
        SliprampCard {
            HStack(alignment: .top, spacing: SliprampTheme.Space.medium) {
                Image(systemName: symbol)
                    .font(.headline)
                    .foregroundStyle(tone.color)
                    .frame(width: 38, height: 38)
                    .background(tone.fill, in: Circle())

                VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(item.title)
                            .font(.headline)
                        Spacer(minLength: SliprampTheme.Space.small)
                        Text(item.occurredAt, style: .relative)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize()
                    }

                    Text(item.description)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)

                    SliprampStatusPill(label: item.actionLabel, tone: tone)
                }

                if showsDisclosure {
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.tertiary)
                        .padding(.top, 12)
                }
            }
        }
    }

    private var symbol: String {
        switch tone {
        case .accent:
            "checkmark.shield.fill"
        case .warning:
            "exclamationmark.triangle.fill"
        default:
            "circle.fill"
        }
    }
}
