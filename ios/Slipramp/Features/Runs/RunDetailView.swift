import SwiftUI

@MainActor
private final class RunDetailViewModel: ObservableObject {
    @Published private(set) var run: Run?
    @Published private(set) var artifacts: [Artifact] = []
    @Published private(set) var isLoading = false
    @Published private(set) var isLoadingArtifacts = false
    @Published private(set) var isMutating = false
    @Published var error: String?
    @Published var artifactError: String?
    @Published var operationError: String?

    private let client: SliprampAPIClient
    private let runID: String
    private let refreshIntervalNanoseconds: UInt64 = 2_000_000_000

    init(client: SliprampAPIClient, runID: String, initialRun: Run?) {
        self.client = client
        self.runID = runID
        run = initialRun
    }

    func load() async {
        guard !isLoading else {
            return
        }
        isLoading = true
        defer { isLoading = false }

        do {
            run = try await client.getRun(id: runID)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }

        await loadArtifacts()
    }

    func loadArtifacts() async {
        guard !isLoadingArtifacts else {
            return
        }
        isLoadingArtifacts = true
        defer { isLoadingArtifacts = false }

        do {
            artifacts = try await client.listRunArtifacts(runID: runID).items
            artifactError = nil
        } catch {
            artifactError = error.localizedDescription
        }
    }

    func followUntilFinished() async -> Bool {
        let startedActive = run?.phase != .finished

        while !Task.isCancelled {
            await load()
            if run?.phase == .finished {
                return startedActive
            }

            do {
                try await Task.sleep(nanoseconds: refreshIntervalNanoseconds)
            } catch is CancellationError {
                return false
            } catch {
                return false
            }
        }

        return false
    }

    func cancel() async -> Bool {
        guard !isMutating else {
            return false
        }
        isMutating = true
        operationError = nil
        defer { isMutating = false }

        do {
            run = try await client.cancelRun(id: runID)
            return true
        } catch {
            operationError = error.localizedDescription
            if case let SliprampAPIError.http(status, _, _) = error, status == 409 {
                await load()
            }
            return false
        }
    }

    func createRerun(mode: RunRerunMode, idempotencyKey: String) async -> Run? {
        guard !isMutating else {
            return nil
        }
        isMutating = true
        operationError = nil
        defer { isMutating = false }

        do {
            return try await client.rerunRun(
                id: runID,
                mode: mode,
                idempotencyKey: idempotencyKey
            )
        } catch {
            operationError = error.localizedDescription
            if case let SliprampAPIError.http(status, _, _) = error, status == 409 {
                await load()
            }
            return nil
        }
    }

    func decideReview(decision: RunReviewDecision, comment: String) async -> Bool {
        guard !isMutating else {
            return false
        }

        let trimmedComment = comment.trimmingCharacters(in: .whitespacesAndNewlines)
        guard trimmedComment.count <= 4096 else {
            operationError = "Review comments cannot exceed 4,096 characters."
            return false
        }

        isMutating = true
        operationError = nil
        defer { isMutating = false }

        do {
            run = try await client.decideRunReview(
                id: runID,
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
}

struct RunDetailView: View {
    @StateObject private var model: RunDetailViewModel
    @State private var showReviewSheet = false
    @State private var showCancelConfirmation = false
    @State private var createdRerun: Run?
    @State private var rerunKey = UUID().uuidString.lowercased()
    @ScaledMetric(relativeTo: .body) private var actionIconWidth: CGFloat = 20

    private let client: SliprampAPIClient
    private let runID: String
    private let onRunChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        runID: String,
        initialRun: Run? = nil,
        onRunChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.runID = runID
        self.onRunChanged = onRunChanged
        _model = StateObject(
            wrappedValue: RunDetailViewModel(client: client, runID: runID, initialRun: initialRun)
        )
    }

    var body: some View {
        Group {
            if let run = model.run {
                List {
                    if let activity = MascotActivityResolver.activity(for: run) {
                        SliprampMascotActivityView(activity: activity, identity: run.id)
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
                    }
                    statusSection(run)
                    reviewSection(run)
                    actionSection(run)
                    parametersSection(run)
                    artifactInputsSection(run)
                    RunArtifactsSection(
                        run: run,
                        artifacts: model.artifacts,
                        isLoading: model.isLoadingArtifacts,
                        error: model.artifactError,
                        client: client,
                        onRetry: {
                            Task { await model.loadArtifacts() }
                        },
                        onRunChanged: onRunChanged
                    )
                    revisionSection(run)
                    timelineSection(run)
                    resultSection(run)
                }
                .scrollContentBackground(.hidden)
                .background(SliprampTheme.canvas)
                .refreshable {
                    await model.load()
                }
            } else if model.isLoading {
                ProgressView("Loading Run…")
            } else {
                ContentUnavailableView {
                    Label("Run unavailable", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(model.error ?? "Slipramp could not load this Run.")
                } actions: {
                    Button("Try again") {
                        Task { await model.load() }
                    }
                }
            }
        }
        .navigationTitle(model.run?.displayAction ?? "Run")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if model.isMutating {
                ToolbarItem(placement: .topBarTrailing) {
                    ProgressView()
                }
            }
        }
        .task {
            if await model.followUntilFinished() {
                onRunChanged()
            }
        }
        .sheet(isPresented: $showReviewSheet) {
            RunReviewDecisionSheet(model: model) {
                onRunChanged()
            }
        }
        .navigationDestination(item: $createdRerun) { run in
            RunDetailView(
                client: client,
                runID: run.id,
                initialRun: run,
                onRunChanged: onRunChanged
            )
        }
    }

    @ViewBuilder
    private func statusSection(_ run: Run) -> some View {
        Section {
            LabeledContent("Status") {
                SliprampStatusPill(
                    label: run.statusLabel,
                    tone: statusTone(run),
                    symbol: statusSymbol(run)
                )
            }
            if let state = run.schedulingState {
                if let blockingRunID = run.blockingRunId {
                    NavigationLink {
                        RunDetailView(
                            client: client,
                            runID: blockingRunID,
                            onRunChanged: onRunChanged
                        )
                    } label: {
                        LabeledContent("Scheduling", value: display(state.rawValue))
                    }
                } else {
                    LabeledContent("Scheduling", value: display(state.rawValue))
                }
            }
            if let state = run.observationState {
                LabeledContent("Observation", value: display(state.rawValue))
            }
            if let requestedAt = run.cancellationRequestedAt, run.phase != .finished {
                LabeledContent("Cancellation requested") {
                    Text(requestedAt, style: .relative)
                        .foregroundStyle(SliprampTone.warning.color)
                }
            }
            if let operationError = model.operationError {
                SliprampCallout(message: operationError, tone: .danger)
            }
        }

        Section("Actions") {
            NavigationLink {
                RunLogView(client: client, runID: runID)
            } label: {
                actionLabel("View live log", systemImage: "terminal")
            }

            if run.phase == .finished {
                Menu {
                    Button {
                        startRerun(.exact)
                    } label: {
                        Label(RunRerunMode.exact.displayLabel, systemImage: "arrow.clockwise")
                            .labelStyle(.titleAndIcon)
                    }

                    Button {
                        startRerun(.latest)
                    } label: {
                        Label(RunRerunMode.latest.displayLabel, systemImage: "play.fill")
                            .labelStyle(.titleAndIcon)
                    }
                    .disabled(!run.inputs.additionalProperties.isEmpty)
                } label: {
                    actionLabel("Run again", systemImage: "arrow.clockwise")
                }
                .disabled(model.isMutating)

                if !run.inputs.additionalProperties.isEmpty {
                    Text("Runs with artifact inputs stay pinned to the producing revision.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            if run.canCancel {
                Button(role: .destructive) {
                    model.clearOperationError()
                    showCancelConfirmation = true
                } label: {
                    actionLabel("Cancel Run", systemImage: "stop.circle")
                }
                .disabled(model.isMutating)
                .confirmationDialog(
                    "Cancel this Run?",
                    isPresented: $showCancelConfirmation,
                    titleVisibility: .visible
                ) {
                    Button("Cancel Run", role: .destructive) {
                        Task {
                            if await model.cancel() {
                                onRunChanged()
                            }
                        }
                    }
                    Button("Keep Running", role: .cancel) {}
                } message: {
                    Text(
                        "Queued Runs stop immediately. Active Runs stop after their Controller "
                            + "removes the execution Job."
                    )
                }
            }
        }
    }

    private func actionLabel(_ title: String, systemImage: String) -> some View {
        HStack(spacing: SliprampTheme.Space.small) {
            Image(systemName: systemImage)
                .frame(width: actionIconWidth)
                .accessibilityHidden(true)
            Text(title)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(title)
    }

    private func statusTone(_ run: Run) -> SliprampTone {
        switch run.outcome {
        case .some(.succeeded):
            .success
        case .some(.failed), .some(.timedOut):
            .danger
        case .some(.canceled):
            .neutral
        default:
            switch run.phase {
            case .running:
                .accent
            case .preparing, .assigned:
                .info
            case .queued, .finished:
                .warning
            }
        }
    }

    private func statusSymbol(_ run: Run) -> String {
        switch run.outcome {
        case .some(.succeeded):
            "checkmark.circle.fill"
        case .some(.failed), .some(.timedOut):
            "xmark.octagon.fill"
        case .some(.canceled):
            "minus.circle.fill"
        default:
            switch run.phase {
            case .running:
                "play.circle.fill"
            case .preparing, .assigned:
                "arrow.triangle.2.circlepath.circle.fill"
            case .queued, .finished:
                "clock.fill"
            }
        }
    }

    @ViewBuilder
    private func reviewSection(_ run: Run) -> some View {
        if run.review.state != .notRequired {
            Section("Review") {
                LabeledContent("State", value: reviewLabel(run.review.state))

                if let actor = run.review.decidedBy {
                    LabeledContent("Decided by", value: actor.displayName)
                }
                if let decidedAt = run.review.decidedAt {
                    TimestampRow(label: "Decided", date: decidedAt)
                }
                if let comment = run.review.comment {
                    Text(comment)
                        .foregroundStyle(.secondary)
                }

                Text(reviewExplanation(run))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                if run.canDecideReview {
                    Button {
                        model.clearOperationError()
                        showReviewSheet = true
                    } label: {
                        Label("Review Run", systemImage: "checkmark.shield")
                            .labelStyle(.titleAndIcon)
                    }
                    .disabled(model.isMutating)
                }
            }
        }
    }

    @ViewBuilder
    private func actionSection(_ run: Run) -> some View {
        Section("Frozen execution context") {
            if let description = run.actionDescription {
                Text(description)
                    .foregroundStyle(.secondary)
            }
            NavigationLink {
                TargetDetailView(
                    client: client,
                    targetID: run.targetId,
                    initialDisplayName: run.target,
                    onRunChanged: onRunChanged
                )
            } label: {
                LabeledContent("Target", value: run.target)
            }
            LabeledContent("Action", value: run.action)
            LabeledContent("Runtime", value: run.runtime)
            LabeledContent("Placement", value: run.placement)
            LabeledContent("Cluster", value: run.cluster)
            if let promotionID = run.source.promotion?.promotionId {
                NavigationLink {
                    PromotionDetailView(
                        client: client,
                        promotionID: promotionID,
                        onPromotionChanged: onRunChanged
                    )
                } label: {
                    LabeledContent("Source", value: run.source.displayLabel)
                }
            } else {
                LabeledContent("Source", value: run.source.displayLabel)
            }
            if let actor = run.createdBy {
                LabeledContent("Created by", value: actor.displayName)
            }
            if let parentRunID = run.rerunOfRunId, let mode = run.rerunMode {
                NavigationLink {
                    RunDetailView(
                        client: client,
                        runID: parentRunID,
                        onRunChanged: onRunChanged
                    )
                } label: {
                    LabeledContent("Run again") {
                        Text(rerunSourceLabel(mode: mode, parentRunID: parentRunID))
                            .multilineTextAlignment(.trailing)
                    }
                }
            }
            if let value = run.actionDocumentationUrl, let url = URL(string: value) {
                Link("Action documentation", destination: url)
            }
        }
    }

    @ViewBuilder
    private func parametersSection(_ run: Run) -> some View {
        let parameters = run.parameters.additionalProperties
        if !parameters.isEmpty {
            Section("Parameters") {
                ForEach(parameters.keys.sorted(), id: \.self) { name in
                    if let value = parameters[name] {
                        LabeledContent(name, value: value.displayValue)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func artifactInputsSection(_ run: Run) -> some View {
        let inputs = run.inputs.additionalProperties
        if !inputs.isEmpty {
            Section("Artifact inputs") {
                ForEach(inputs.keys.sorted(), id: \.self) { name in
                    if let input = inputs[name] {
                        NavigationLink {
                            RunDetailView(
                                client: client,
                                runID: input.producerRunId,
                                onRunChanged: onRunChanged
                            )
                        } label: {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(name)
                                Text("\(input.name) from Run \(input.producerRunId.prefix(8))")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func revisionSection(_ run: Run) -> some View {
        Section("Revision") {
            LabeledContent("Commit") {
                Text(run.commit.shortSHA)
                    .fontDesign(.monospaced)
            }
            if let title = run.commit.title {
                Text(title)
            }
        }
    }

    @ViewBuilder
    private func timelineSection(_ run: Run) -> some View {
        Section("Timeline") {
            TimestampRow(label: "Created", date: run.createdAt)
            TimestampRow(label: "Assigned", date: run.assignedAt)
            TimestampRow(label: "Preparing", date: run.preparingAt)
            TimestampRow(label: "Started", date: run.startedAt)
            TimestampRow(label: "Finished", date: run.finishedAt)
        }
    }

    @ViewBuilder
    private func resultSection(_ run: Run) -> some View {
        if run.outcomeReason != nil || run.outcomeMessage != nil || run.exitCode != nil {
            Section("Result") {
                if let reason = run.outcomeReason {
                    LabeledContent("Reason", value: reason)
                }
                if let exitCode = run.exitCode {
                    LabeledContent("Exit code", value: String(exitCode))
                }
                if let message = run.outcomeMessage {
                    Text(message)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func display(_ value: String) -> String {
        value.replacingOccurrences(of: "-", with: " ").capitalized
    }

    private func reviewLabel(_ state: Components.Schemas.RunReviewState) -> String {
        switch state {
        case .notRequired:
            "Not required"
        case .pending:
            "Awaiting review"
        case .approved:
            "Approved"
        case .rejected:
            "Rejected"
        }
    }

    private func reviewExplanation(_ run: Run) -> String {
        switch run.review.state {
        case .notRequired:
            "This Run did not require a review."
        case .pending where run.phase == .queued:
            "This Git-triggered Run stays queued until an operator approves or rejects it."
        case .pending:
            "No review decision was recorded before this Run left the queue."
        case .approved:
            "Approval made this Run eligible for assignment; it does not indicate execution succeeded."
        case .rejected:
            "Rejection canceled this Run before assignment."
        }
    }

    private func startRerun(_ mode: RunRerunMode) {
        let idempotencyKey = "\(rerunKey)-\(mode.rawValue)"
        Task {
            guard let run = await model.createRerun(
                mode: mode,
                idempotencyKey: idempotencyKey
            ) else {
                return
            }

            rerunKey = UUID().uuidString.lowercased()
            onRunChanged()
            createdRerun = run
        }
    }

    private func rerunSourceLabel(mode: RunRerunMode, parentRunID: String) -> String {
        let relationship = mode == .exact ? "Same revision as" : "Latest revision from"
        return "\(relationship) Run \(parentRunID.prefix(8))"
    }
}

private struct RunReviewDecisionSheet: View {
    @ObservedObject var model: RunDetailViewModel
    @Environment(\.dismiss) private var dismiss
    @State private var comment = ""
    @State private var decisionToConfirm: RunReviewDecision?

    let onCompleted: @MainActor () -> Void

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(
                        "The decision is recorded permanently with this Run. "
                            + "Review the frozen execution context before continuing."
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
            .navigationTitle("Review Run")
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

    private func confirmationPresented(for decision: RunReviewDecision) -> Binding<Bool> {
        Binding(
            get: { decisionToConfirm == decision },
            set: { presented in
                if !presented, decisionToConfirm == decision {
                    decisionToConfirm = nil
                }
            }
        )
    }

    private func decisionButton(_ decision: RunReviewDecision) -> some View {
        Button(role: decision == .rejected ? .destructive : nil) {
            decisionToConfirm = decision
        } label: {
            Label(
                decision == .approved ? "Approve Run" : "Reject Run",
                systemImage: decision == .approved ? "checkmark.circle" : "xmark.circle"
            )
            .labelStyle(.titleAndIcon)
        }
        .confirmationDialog(
            decision == .approved ? "Approve this Run?" : "Reject this Run?",
            isPresented: confirmationPresented(for: decision),
            titleVisibility: .visible
        ) {
            Button(
                decision == .approved ? "Approve Run" : "Reject Run",
                role: decision == .rejected ? .destructive : nil
            ) {
                decisionToConfirm = nil
                Task {
                    if await model.decideReview(decision: decision, comment: comment) {
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

    private func confirmationMessage(for decision: RunReviewDecision) -> String {
        if decision == .approved {
            return "Approval makes this Run eligible for ordinary assignment."
        }
        return "Rejection cancels this Run before assignment and cannot be undone."
    }
}

private struct TimestampRow: View {
    let label: String
    let date: Date?

    var body: some View {
        if let date {
            LabeledContent(label) {
                Text(date, format: .dateTime.month().day().hour().minute().second())
            }
        }
    }
}
