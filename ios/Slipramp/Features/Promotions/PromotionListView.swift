import SwiftUI

@MainActor
private final class PromotionListViewModel: ObservableObject {
    @Published private(set) var promotions: [Promotion] = []
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
            let response = try await client.listPromotions()
            promotions = response.items
            total = response.total
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct PromotionListView: View {
    @StateObject private var model: PromotionListViewModel

    private let client: SliprampAPIClient
    private let refreshRevision: Int
    private let onPromotionChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        refreshRevision: Int = 0,
        onPromotionChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.refreshRevision = refreshRevision
        self.onPromotionChanged = onPromotionChanged
        _model = StateObject(wrappedValue: PromotionListViewModel(client: client))
    }

    var body: some View {
        ZStack {
            SliprampScreenBackground()

            Group {
                if !model.promotions.isEmpty {
                    ScrollView {
                        LazyVStack(spacing: SliprampTheme.Space.medium) {
                            ForEach(model.promotions) { promotion in
                                NavigationLink {
                                    PromotionDetailView(
                                        client: client,
                                        promotionID: promotion.id,
                                        initialPromotion: promotion,
                                        onPromotionChanged: onPromotionChanged
                                    )
                                } label: {
                                    PromotionRow(promotion: promotion)
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
                    SliprampLoadingState(message: "Loading Promotions…")
                } else if let error = model.error {
                    SliprampEmptyState(
                        title: "Promotions unavailable",
                        message: error,
                        symbol: "exclamationmark.triangle.fill",
                        tone: .danger,
                        actionTitle: "Try again"
                    ) {
                        Task { await model.load() }
                    }
                } else {
                    SliprampEmptyState(
                        title: "No Promotions yet",
                        message: "Kargo Promotions will appear here after they are submitted.",
                        symbol: "arrow.up.right.circle",
                        tone: .accent
                    )
                }
            }
        }
        .navigationTitle("Promotions")
        .toolbar {
            if model.total > model.promotions.count {
                ToolbarItem(placement: .topBarTrailing) {
                    Text("\(model.promotions.count) of \(model.total)")
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

private struct PromotionRow: View {
    let promotion: Promotion

    var body: some View {
        SliprampCard {
            HStack(alignment: .top, spacing: SliprampTheme.Space.medium) {
                Image(systemName: promotion.statusSymbol)
                    .foregroundStyle(promotion.statusTone.color)
                    .font(.headline)
                    .frame(width: 40, height: 40)
                    .background(promotion.statusTone.fill, in: Circle())

                VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(promotion.externalPromotionId)
                            .font(.headline)
                            .lineLimit(2)
                        Spacer(minLength: SliprampTheme.Space.small)
                        Text(promotion.createdAt, style: .relative)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize()
                    }

                    Text(
                        "\(promotion.stageSnapshot.application) · "
                            + "\(promotion.stageSnapshot.project)/\(promotion.stageSnapshot.name)"
                    )
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)

                    SliprampStatusPill(
                        label: promotion.statusLabel,
                        tone: promotion.statusTone,
                        symbol: promotion.statusSymbol
                    )

                    Text("\(promotion.source.displayLabel) · \(promotion.requestedBy.displayName)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }

                Image(systemName: "chevron.right")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.tertiary)
                    .padding(.top, 13)
            }
        }
    }
}

extension Components.Schemas.Promotion {
    var statusTone: SliprampTone {
        switch status {
        case .creating, .preparing:
            return .info
        case .pending:
            return .warning
        case .approved:
            guard let kargoOutcome else {
                return .info
            }
            return kargoOutcome.phase == .succeeded ? .success : .danger
        case .rejected, .error:
            return .danger
        case .expired:
            return .neutral
        }
    }

    var statusSymbol: String {
        switch status {
        case .creating, .preparing:
            return "arrow.triangle.2.circlepath"
        case .pending:
            return "checkmark.shield.fill"
        case .approved:
            guard let kargoOutcome else {
                return "arrow.triangle.2.circlepath"
            }
            return kargoOutcome.phase == .succeeded
                ? "checkmark.circle.fill" : "xmark.octagon.fill"
        case .rejected, .error:
            return "xmark.octagon.fill"
        case .expired:
            return "clock.badge.xmark"
        }
    }
}
