import SwiftUI

@MainActor
private final class PromotionDetailViewModel: ObservableObject {
    @Published private(set) var promotion: Promotion?
    @Published private(set) var gitDiff: String?
    @Published private(set) var argoDiff: ArgoDiffDocument?
    @Published private(set) var isLoading = false
    @Published private(set) var isLoadingDiffs = false
    @Published private(set) var isMutating = false
    @Published var error: String?
    @Published var gitDiffError: String?
    @Published var argoDiffError: String?
    @Published var operationError: String?

    private let client: SliprampAPIClient
    private let promotionID: String

    init(client: SliprampAPIClient, promotionID: String) {
        self.client = client
        self.promotionID = promotionID
    }

    func load() async {
        guard !isLoading else {
            return
        }
        isLoading = true
        defer { isLoading = false }

        do {
            let loadedPromotion = try await client.getPromotion(id: promotionID)
            promotion = loadedPromotion
            error = nil
            await loadDiffs(for: loadedPromotion)
        } catch {
            self.error = error.localizedDescription
        }
    }

    func decide(_ decision: PromotionDecisionChoice, comment: String) async -> Bool {
        guard !isMutating else {
            return false
        }

        let trimmedComment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmedComment.count <= 4096 else {
            operationError = "Decision comments cannot exceed 4,096 characters."
            return false
        }

        isMutating = true
        operationError = nil
        defer { isMutating = false }

        do {
            promotion = try await client.decidePromotion(
                id: promotionID,
                decision: decision,
                comment: trimmedComment.isEmpty ? nil : trimmedComment
            )
            return true
        } catch {
            operationError = error.localizedDescription
            if case let SliprampAPIError.http(status, _, _) = error, status == 409 {
                await load()
            }
            return false
        }
    }

    func clearOperationError() {
        operationError = nil
    }

    private func loadDiffs(for promotion: Promotion) async {
        let gitID = promotion.gitDiffArtifactId
        let argoID = promotion.argoDiffArtifactId

        gitDiff = nil
        argoDiff = nil
        gitDiffError = nil
        argoDiffError = nil
        guard gitID != nil || argoID != nil else {
            return
        }

        isLoadingDiffs = true
        defer { isLoadingDiffs = false }

        async let gitLoad = loadArtifactText(client: client, id: gitID)
        async let argoLoad = loadArtifactText(client: client, id: argoID)
        let (loadedGit, loadedArgo) = await (gitLoad, argoLoad)

        gitDiff = loadedGit.contents
        gitDiffError = loadedGit.error
        argoDiffError = loadedArgo.error

        if let argoContents = loadedArgo.contents {
            do {
                argoDiff = try JSONDecoder().decode(
                    ArgoDiffDocument.self,
                    from: Data(argoContents.utf8)
                )
            } catch {
                argoDiffError = "The Argo CD diff artifact is malformed: \(error.localizedDescription)"
            }
        }
    }
}

private struct ArtifactTextLoad: Sendable {
    let contents: String?
    let error: String?
}

private func loadArtifactText(client: SliprampAPIClient, id: String?) async -> ArtifactTextLoad {
    guard let id else {
        return ArtifactTextLoad(contents: nil, error: nil)
    }

    do {
        return ArtifactTextLoad(contents: try await client.downloadArtifactText(id: id), error: nil)
    } catch {
        return ArtifactTextLoad(contents: nil, error: error.localizedDescription)
    }
}

struct PromotionDetailView: View {
    @StateObject private var model: PromotionDetailViewModel
    @State private var showDecisionSheet = false

    private let client: SliprampAPIClient
    private let onPromotionChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        promotionID: String,
        onPromotionChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.onPromotionChanged = onPromotionChanged
        _model = StateObject(
            wrappedValue: PromotionDetailViewModel(client: client, promotionID: promotionID)
        )
    }

    var body: some View {
        Group {
            if let promotion = model.promotion {
                List {
                    SliprampMascotActivityView(
                        activity: MascotActivityResolver.activity(for: promotion),
                        identity: promotion.id
                    )
                    .listRowInsets(
                        EdgeInsets(
                            top: SliprampTheme.Space.small,
                            leading: SliprampTheme.Space.large,
                            bottom: SliprampTheme.Space.small,
                            trailing: SliprampTheme.Space.large
                        )
                    )
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                    statusSection(promotion)
                    contextSection(promotion)
                    revisionSection(promotion)
                    freightSection(promotion)
                    preparationSection(promotion)
                    diffsSection(promotion)
                    decisionSection(promotion)
                    timelineSection(promotion)
                }
                .scrollContentBackground(.hidden)
                .background(SliprampTheme.canvas)
                .refreshable {
                    await model.load()
                }
            } else if model.isLoading {
                ProgressView("Loading Promotion…")
            } else {
                ContentUnavailableView {
                    Label("Promotion unavailable", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(model.error ?? "Slipramp could not load this Promotion.")
                } actions: {
                    Button("Try again") {
                        Task { await model.load() }
                    }
                }
            }
        }
        .navigationTitle(model.promotion?.externalPromotionId ?? "Promotion")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if model.isMutating {
                ToolbarItem(placement: .topBarTrailing) {
                    ProgressView()
                }
            }
        }
        .task {
            await model.load()
        }
        .sheet(isPresented: $showDecisionSheet) {
            PromotionDecisionSheet(model: model) {
                onPromotionChanged()
            }
        }
    }

    @ViewBuilder
    private func statusSection(_ promotion: Promotion) -> some View {
        Section {
            LabeledContent("Status") {
                SliprampStatusPill(
                    label: promotion.statusLabel,
                    tone: statusTone(promotion),
                    symbol: statusSymbol(promotion)
                )
            }
            LabeledContent("Source", value: promotion.source.displayLabel)
            LabeledContent("Requested by", value: promotion.requestedBy.displayName)
            if let message = promotion.message {
                Label(message, systemImage: "exclamationmark.triangle.fill")
                    .font(.subheadline)
                    .foregroundStyle(
                        promotion.status == .expired ? Color.secondary : SliprampTone.danger.color
                    )
            }
            if let value = promotion.kargoUrl, let url = URL(string: value) {
                Link("Open in Kargo", destination: url)
            }
        }
    }

    private func statusTone(_ promotion: Promotion) -> SliprampTone {
        switch promotion.status {
        case .creating, .preparing:
            .info
        case .pending:
            .warning
        case .approved:
            .success
        case .rejected, .error:
            .danger
        case .expired:
            .neutral
        }
    }

    private func statusSymbol(_ promotion: Promotion) -> String {
        switch promotion.status {
        case .creating, .preparing:
            "arrow.triangle.2.circlepath"
        case .pending:
            "checkmark.shield.fill"
        case .approved:
            "checkmark.circle.fill"
        case .rejected, .error:
            "xmark.octagon.fill"
        case .expired:
            "clock.badge.xmark"
        }
    }

    @ViewBuilder
    private func contextSection(_ promotion: Promotion) -> some View {
        let stage = promotion.stageSnapshot
        let repository = promotion.repositorySnapshot

        Section("Promotion context") {
            LabeledContent("Integration", value: promotion.integration)
            LabeledContent("Stage", value: "\(stage.project)/\(stage.name)")
            LabeledContent("Application", value: stage.application)
            LabeledContent("Environment", value: stage.environment)
            LabeledContent("Cluster", value: stage.cluster)
            LabeledContent("Repository", value: repository.name)
            LabeledContent("Policy", value: promotion.policySnapshot.effectiveMode.displayLabel)
            LabeledContent("Target branch", value: promotion.targetBranch)
            LabeledContent("Candidate branch", value: promotion.candidateBranch ?? "Waiting for Kargo")
            if let expiresAt = promotion.expiresAt {
                PromotionTimestampRow(label: "Review expires", date: expiresAt)
            }
        }
    }

    @ViewBuilder
    private func revisionSection(_ promotion: Promotion) -> some View {
        Section("Revisions") {
            PromotionRevisionRow(label: "Base", commit: promotion.baseCommit)
            PromotionRevisionRow(label: "Candidate", commit: promotion.candidateCommit)
        }
    }

    @ViewBuilder
    private func freightSection(_ promotion: Promotion) -> some View {
        if let freight = promotion.selectedFreight {
            Section("Selected Freight") {
                LabeledContent("Name", value: freight.name)
                LabeledContent("Origin", value: freight.origin)
                ForEach(Array(freight.artifacts.enumerated()), id: \.offset) { _, artifact in
                    VStack(alignment: .leading, spacing: 3) {
                        Text(artifact.kind.rawValue.capitalized)
                        Text(
                            [artifact.repository, artifact.version]
                                .compactMap { $0 }
                                .joined(separator: " · ")
                        )
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func preparationSection(_ promotion: Promotion) -> some View {
        Section("Preparation Run") {
            if let run = promotion.preparationRun {
                NavigationLink {
                    RunDetailView(
                        client: client,
                        runID: run.id,
                        initialRun: run,
                        onRunChanged: refreshAfterRunChange
                    )
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Run \(run.id.prefix(8))")
                        Text("\(run.displayAction) · \(run.statusLabel)")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            } else {
                Text(
                    promotion.reviewRequired
                        ? "The preparation Run has not been attached yet."
                        : "This Promotion did not require preparation."
                )
                .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private func diffsSection(_ promotion: Promotion) -> some View {
        Section("Prepared diffs") {
            if model.isLoadingDiffs {
                ProgressView("Loading sensitive diff artifacts…")
            }

            if promotion.gitDiffArtifactId != nil {
                if let gitDiff = model.gitDiff {
                    NavigationLink {
                        GitDiffView(contents: gitDiff)
                    } label: {
                        ArtifactPreviewLabel(
                            title: "Git diff",
                            description: promotion.gitDiffArtifact?.description,
                            summary: gitDiff.isEmpty ? "No Git changes" : "\(lineCount(gitDiff)) lines"
                        )
                    }
                } else if let error = model.gitDiffError {
                    DiffErrorLabel(title: "Git diff unavailable", error: error)
                }
            }

            if promotion.argoDiffArtifactId != nil {
                if let argoDiff = model.argoDiff {
                    NavigationLink {
                        ArgoDiffView(document: argoDiff)
                    } label: {
                        ArtifactPreviewLabel(
                            title: "Argo CD resources",
                            description: promotion.argoDiffArtifact?.description,
                            summary: "\(argoDiff.resources.count) resource changes"
                        )
                    }
                } else if let error = model.argoDiffError {
                    DiffErrorLabel(title: "Argo CD diff unavailable", error: error)
                }
            }

            if promotion.gitDiffArtifactId == nil && promotion.argoDiffArtifactId == nil {
                Text(diffUnavailableExplanation(promotion))
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private func decisionSection(_ promotion: Promotion) -> some View {
        Section("Decision") {
            if let decision = promotion.decision {
                LabeledContent(
                    "Decision",
                    value: decision.decision.rawValue.capitalized
                )
                LabeledContent("Decided by", value: decision.decidedBy.displayName)
                PromotionTimestampRow(label: "Decided", date: decision.decidedAt)
                if let comment = decision.comment {
                    Text(comment)
                        .foregroundStyle(.secondary)
                }
                Text("This attribution is immutable.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else if promotion.canDecide {
                Text("Review the prepared changes before approving or rejecting this Promotion.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                Button {
                    model.clearOperationError()
                    showDecisionSheet = true
                } label: {
                    Label("Review Promotion", systemImage: "checkmark.shield")
                        .labelStyle(.titleAndIcon)
                }
                .disabled(model.isMutating)
            } else {
                Text(decisionExplanation(promotion))
                    .foregroundStyle(.secondary)
            }

            if let operationError = model.operationError {
                Label(operationError, systemImage: "exclamationmark.triangle.fill")
                    .font(.subheadline)
                    .foregroundStyle(SliprampTone.danger.color)
            }
        }
    }

    @ViewBuilder
    private func timelineSection(_ promotion: Promotion) -> some View {
        Section("Timeline") {
            PromotionTimestampRow(label: "Created", date: promotion.createdAt)
            if let completedAt = promotion.preparationCompletedAt {
                PromotionTimestampRow(label: "Prepared", date: completedAt)
            }
            if let decidedAt = promotion.decidedAt {
                PromotionTimestampRow(label: "Decided", date: decidedAt)
            }
            PromotionTimestampRow(label: "Updated", date: promotion.updatedAt)
        }
    }

    private func refreshAfterRunChange() {
        onPromotionChanged()
        Task { await model.load() }
    }

    private func lineCount(_ value: String) -> Int {
        value.isEmpty ? 0 : value.split(separator: "\n", omittingEmptySubsequences: false).count
    }

    private func diffUnavailableExplanation(_ promotion: Promotion) -> String {
        switch promotion.status {
        case .creating, .preparing:
            "The diff artifacts are still being prepared. Pull to refresh."
        default:
            "This Promotion has no prepared diff artifacts."
        }
    }

    private func decisionExplanation(_ promotion: Promotion) -> String {
        switch promotion.status {
        case .expired:
            "The review window expired before a decision was recorded."
        case .creating, .preparing:
            "This Promotion is not ready for a decision yet."
        default:
            "This Promotion is not awaiting an operator decision."
        }
    }
}

private struct ArtifactPreviewLabel: View {
    let title: String
    let description: String?
    let summary: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
            if let description {
                Text(description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Text(summary)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

private struct DiffErrorLabel: View {
    let title: String
    let error: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Label(title, systemImage: "exclamationmark.triangle.fill")
                .foregroundStyle(SliprampTone.warning.color)
            Text(error)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

private struct PromotionRevisionRow: View {
    let label: String
    let commit: Components.Schemas.GitCommit?

    var body: some View {
        if let commit {
            VStack(alignment: .leading, spacing: 3) {
                LabeledContent(label) {
                    Text(commit.shortSHA)
                        .fontDesign(.monospaced)
                }
                if let title = commit.title {
                    Text(title)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } else {
            LabeledContent(label, value: "Waiting for Kargo")
        }
    }
}

private struct PromotionTimestampRow: View {
    let label: String
    let date: Date

    var body: some View {
        LabeledContent(label) {
            Text(date, format: .dateTime.month().day().hour().minute().second())
        }
    }
}

private struct GitDiffView: View {
    let contents: String

    var body: some View {
        Group {
            if contents.isEmpty {
                ContentUnavailableView(
                    "No Git changes",
                    systemImage: "checkmark.circle",
                    description: Text("The prepared candidate does not change tracked Git content.")
                )
            } else {
                ScrollView([.horizontal, .vertical]) {
                    Text(contents)
                        .font(.system(.caption, design: .monospaced))
                        .textSelection(.enabled)
                        .padding()
                }
            }
        }
        .navigationTitle("Git diff")
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct ArgoDiffView: View {
    let document: ArgoDiffDocument

    @State private var query = ""
    @State private var showVersionMetadata = false

    var body: some View {
        List {
            Section {
                TextField("Filter by kind or name", text: $query)
                    .textInputAutocapitalization(.never)
                Toggle(
                    "Show version-only changes (\(hiddenVersionCount))",
                    isOn: $showVersionMetadata
                )
            }

            Section("Resources") {
                if filteredResources.isEmpty {
                    Text(emptyMessage)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(filteredResources) { resource in
                        NavigationLink {
                            ArgoResourceDiffView(resource: resource)
                        } label: {
                            VStack(alignment: .leading, spacing: 3) {
                                Text("\(resource.kind) / \(resource.name)")
                                if let namespace = resource.namespace {
                                    Text(namespace)
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                                if resource.versionMetadataOnly {
                                    Text("Version metadata only")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                }
            }
        }
        .scrollContentBackground(.hidden)
        .background(SliprampTheme.canvas)
        .navigationTitle("Argo CD diff")
        .navigationBarTitleDisplayMode(.inline)
    }

    private var filteredResources: [ArgoDiffResource] {
        let normalizedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return document.resources.filter { resource in
            guard showVersionMetadata || !resource.versionMetadataOnly else {
                return false
            }
            guard !normalizedQuery.isEmpty else {
                return true
            }
            return [resource.group, resource.kind, resource.namespace, resource.name]
                .compactMap { $0?.lowercased() }
                .contains { $0.contains(normalizedQuery) }
        }
    }

    private var hiddenVersionCount: Int {
        document.resources.filter(\.versionMetadataOnly).count
    }

    private var emptyMessage: String {
        document.resources.isEmpty ? "No Argo CD resource changes." : "No resources match the filter."
    }
}

private struct ArgoResourceDiffView: View {
    let resource: ArgoDiffResource

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(resource.kind)
                    .font(.headline)
                Text([resource.group, resource.namespace, resource.name].compactMap { $0 }.joined(separator: " / "))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if resource.versionMetadataOnly {
                    Label("Version metadata only", systemImage: "info.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal)

            ScrollView([.horizontal, .vertical]) {
                Text(resource.diff.isEmpty ? "No textual diff." : resource.diff)
                    .font(.system(.caption, design: .monospaced))
                    .textSelection(.enabled)
                    .padding()
            }
        }
        .padding(.top)
        .navigationTitle(resource.name)
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct PromotionDecisionSheet: View {
    @ObservedObject var model: PromotionDetailViewModel
    @Environment(\.dismiss) private var dismiss
    @State private var comment = ""
    @State private var decisionToConfirm: PromotionDecisionChoice?

    let onCompleted: @MainActor () -> Void

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "The decision is permanent. Review the prepared Git and Argo CD changes "
                            + "before continuing."
                    )
                    .foregroundStyle(.secondary)
                }

                Section("Optional comment") {
                    TextField(
                        "Context for this decision",
                        text: $comment,
                        axis: .vertical
                    )
                    .lineLimit(3 ... 8)

                    Text("\(comment.count) / 4,096")
                        .font(.caption)
                        .foregroundStyle(
                            comment.count > 4096 ? SliprampTone.danger.color : Color.secondary
                        )
                }

                if let operationError = model.operationError {
                    Section {
                        Label(operationError, systemImage: "exclamationmark.triangle.fill")
                            .foregroundStyle(SliprampTone.danger.color)
                    }
                }

                Section {
                    decisionButton(.rejected)
                    decisionButton(.approved)
                }
                .disabled(model.isMutating || comment.count > 4096)
            }
            .navigationTitle("Review Promotion")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") {
                        dismiss()
                    }
                    .disabled(model.isMutating)
                }
                if model.isMutating {
                    ToolbarItem(placement: .confirmationAction) {
                        ProgressView()
                    }
                }
            }
        }
    }

    private func confirmationPresented(for decision: PromotionDecisionChoice) -> Binding<Bool> {
        Binding(
            get: { decisionToConfirm == decision },
            set: { presented in
                if !presented, decisionToConfirm == decision {
                    decisionToConfirm = nil
                }
            }
        )
    }

    private func decisionButton(_ decision: PromotionDecisionChoice) -> some View {
        Button(role: decision == .rejected ? .destructive : nil) {
            decisionToConfirm = decision
        } label: {
            Label(
                decision == .approved ? "Approve Promotion" : "Reject Promotion",
                systemImage: decision == .approved ? "checkmark.circle" : "xmark.circle"
            )
            .labelStyle(.titleAndIcon)
        }
        .confirmationDialog(
            decision == .approved ? "Approve this Promotion?" : "Reject this Promotion?",
            isPresented: confirmationPresented(for: decision),
            titleVisibility: .visible
        ) {
            Button(
                decision == .approved ? "Approve Promotion" : "Reject Promotion",
                role: decision == .rejected ? .destructive : nil
            ) {
                decisionToConfirm = nil
                Task {
                    if await model.decide(decision, comment: comment) {
                        onCompleted()
                        dismiss()
                    }
                }
            }
            Button("Go Back", role: .cancel) {
                decisionToConfirm = nil
            }
        } message: {
            Text(confirmationMessage(for: decision))
        }
    }

    private func confirmationMessage(for decision: PromotionDecisionChoice) -> String {
        if decision == .approved {
            return "Approval lets Kargo continue; it does not mean the downstream Promotion succeeded."
        }
        return "Rejection records the decision permanently and cannot be undone."
    }
}
