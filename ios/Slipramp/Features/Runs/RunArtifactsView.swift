import QuickLook
import SwiftUI
import UniformTypeIdentifiers

struct RunArtifactsSection: View {
    let run: Run
    let artifacts: [Artifact]
    let isLoading: Bool
    let error: String?
    let client: SliprampAPIClient
    let onRetry: () -> Void
    let onRunChanged: @MainActor () -> Void

    var body: some View {
        Section("Artifacts") {
            if let error {
                SliprampCallout(message: error, tone: .danger)
                Button("Try again", action: onRetry)
            }

            if artifacts.isEmpty {
                if isLoading {
                    HStack(spacing: SliprampTheme.Space.small) {
                        ProgressView()
                        Text("Loading Artifacts…")
                            .foregroundStyle(.secondary)
                    }
                } else if error == nil {
                    Text(emptyMessage)
                        .foregroundStyle(.secondary)
                }
            } else {
                ForEach(artifacts) { artifact in
                    NavigationLink {
                        RunArtifactDetailView(
                            client: client,
                            run: run,
                            artifact: artifact,
                            onRunChanged: onRunChanged
                        )
                    } label: {
                        ArtifactRow(artifact: artifact)
                    }
                }
            }
        }
    }

    private var emptyMessage: String {
        run.phase == .finished
            ? "No Artifacts were published."
            : "No Artifacts have been published yet."
    }
}

private struct ArtifactRow: View {
    let artifact: Artifact

    var body: some View {
        HStack(alignment: .top, spacing: SliprampTheme.Space.small) {
            VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
                Text(artifact.name)
                    .font(.headline)

                Text(artifact.mediaType)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)

                HStack(spacing: SliprampTheme.Space.small) {
                    Text(formatBytes(artifact.sizeBytes))
                    if artifact.sensitive {
                        Label("Sensitive", systemImage: "lock.fill")
                            .foregroundStyle(SliprampTone.warning.color)
                    }
                }
                .font(.caption)
            }

            Spacer(minLength: SliprampTheme.Space.small)

            SliprampStatusPill(
                label: artifact.statusLabel,
                tone: artifactTone(artifact),
                symbol: artifactSymbol(artifact)
            )
        }
        .padding(.vertical, SliprampTheme.Space.xSmall)
    }
}

private struct RunArtifactDetailView: View {
    @State private var operationError: String?
    @State private var isDownloading = false
    @State private var isStarting = false
    @State private var showStartConfirmation = false
    @State private var pendingConsumer: RunArtifactConsumer?
    @State private var previewURL: URL?
    @State private var downloadedURL: URL?
    @State private var createdRun: Run?
    @State private var idempotencyKey = UUID().uuidString.lowercased()

    let client: SliprampAPIClient
    let run: Run
    let artifact: Artifact
    let onRunChanged: @MainActor () -> Void

    var body: some View {
        List {
            statusSection
            metadataSection
            provenanceSection
            contentSection
            consumerSection
        }
        .scrollContentBackground(.hidden)
        .background(SliprampTheme.canvas)
        .navigationTitle(artifact.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if isDownloading || isStarting {
                ToolbarItem(placement: .topBarTrailing) {
                    ProgressView()
                }
            }
        }
        .confirmationDialog(
            "Start \(pendingConsumer?.actionTitle ?? pendingConsumer?.action ?? "Action")?",
            isPresented: $showStartConfirmation,
            titleVisibility: .visible
        ) {
            Button("Start Run") {
                guard let pendingConsumer else {
                    return
                }
                Task { await startAction(pendingConsumer) }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            if let pendingConsumer {
                Text(
                    "This binds \(artifact.name) to input \(pendingConsumer.input) and pins the new Run "
                        + "to commit \(artifact.provenance.commit.shortSHA)."
                )
            }
        }
        .quickLookPreview($previewURL)
        .navigationDestination(item: $createdRun) { run in
            RunDetailView(
                client: client,
                runID: run.id,
                initialRun: run,
                onRunChanged: onRunChanged
            )
        }
        .onChange(of: previewURL) { _, value in
            if value == nil {
                removeDownloadedArtifact()
            }
        }
        .onDisappear {
            if previewURL == nil {
                removeDownloadedArtifact()
            }
        }
    }

    private var statusSection: some View {
        Section {
            LabeledContent("Status") {
                SliprampStatusPill(
                    label: artifact.statusLabel,
                    tone: artifactTone(artifact),
                    symbol: artifactSymbol(artifact)
                )
            }
            if artifact.sensitive {
                SliprampCallout(
                    message: "This Artifact contains sensitive content. Opening it requires sufficient access "
                        + "and may expose it to other apps through the share sheet.",
                    tone: .warning,
                    symbol: "lock.fill"
                )
            }
            if let operationError {
                SliprampCallout(message: operationError, tone: .danger)
            }
        }
    }

    private var metadataSection: some View {
        Section("Artifact") {
            if let description = artifact.description {
                Text(description)
                    .foregroundStyle(.secondary)
            }
            LabeledContent("Kind", value: display(artifact.kind.rawValue))
            LabeledContent("Media type") {
                Text(artifact.mediaType)
                    .fontDesign(.monospaced)
                    .textSelection(.enabled)
            }
            LabeledContent("Size", value: formatBytes(artifact.sizeBytes))
            LabeledContent("Retention", value: display(artifact.retentionState.rawValue))
            if let digest = artifact.sha256 {
                LabeledContent("SHA-256") {
                    Text(digest)
                        .font(.caption.monospaced())
                        .multilineTextAlignment(.trailing)
                        .textSelection(.enabled)
                }
            }
            LabeledContent("Created") {
                Text(artifact.createdAt.formatted(date: .abbreviated, time: .shortened))
            }
            if let sealedAt = artifact.sealedAt {
                LabeledContent("Sealed") {
                    Text(sealedAt.formatted(date: .abbreviated, time: .shortened))
                }
            }
        }
    }

    private var provenanceSection: some View {
        Section("Provenance") {
            LabeledContent("Target", value: artifact.provenance.target)
            LabeledContent("Action", value: artifact.provenance.action)
            LabeledContent("Commit") {
                Text(artifact.provenance.commit.shortSHA)
                    .fontDesign(.monospaced)
            }
            LabeledContent("Catalog revision") {
                Text(String(artifact.provenance.catalogRevisionId.prefix(8)))
                    .fontDesign(.monospaced)
            }
        }
    }

    @ViewBuilder
    private var contentSection: some View {
        Section("Content") {
            if artifact.isDownloadable {
                Button {
                    Task { await openArtifact() }
                } label: {
                    Label("Open Artifact", systemImage: "doc.viewfinder")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .tint(SliprampTheme.accent)
                .controlSize(.large)
                .disabled(isDownloading || isStarting)

                Text(
                    "The content is downloaded, verified, and opened with the system preview. "
                        + "Its share menu can save or send a copy."
                )
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else {
                SliprampCallout(
                    message: unavailableContentMessage,
                    tone: artifact.state == .failed ? .danger : .neutral,
                    symbol: artifact.state == .failed ? "xmark.octagon.fill" : "clock.fill"
                )
            }
        }
    }

    @ViewBuilder
    private var consumerSection: some View {
        let consumers = eligibleConsumers
        if !consumers.isEmpty {
            Section("Compatible Actions") {
                ForEach(consumers, id: \.action) { consumer in
                    Button {
                        operationError = nil
                        pendingConsumer = consumer
                        showStartConfirmation = true
                    } label: {
                        Label(
                            "Start \(consumer.actionTitle ?? consumer.action)",
                            systemImage: "play.fill"
                        )
                        .labelStyle(.titleAndIcon)
                    }
                    .disabled(isDownloading || isStarting)

                    Text("Uses this Artifact as input \(consumer.input).")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private var eligibleConsumers: [RunArtifactConsumer] {
        guard run.outcome == .succeeded, artifact.isDownloadable else {
            return []
        }
        return run.artifactConsumers.additionalProperties[artifact.name] ?? []
    }

    private var unavailableContentMessage: String {
        if artifact.retentionState != .retained {
            return "This Artifact is \(artifact.retentionState.rawValue) and its content is no longer available."
        }
        switch artifact.state {
        case .open:
            return "This Artifact is still being written and cannot be opened yet."
        case .failed:
            return "Artifact publication failed, so no complete content is available."
        case .sealed:
            return "This Artifact is unavailable."
        }
    }

    @MainActor
    private func openArtifact() async {
        guard !isDownloading else {
            return
        }
        if let downloadedURL {
            previewURL = downloadedURL
            return
        }

        isDownloading = true
        operationError = nil
        defer { isDownloading = false }

        do {
            let destination = artifactDownloadURL
            try? FileManager.default.removeItem(at: destination)
            try await client.downloadArtifact(
                id: artifact.id,
                to: destination,
                expectedSHA256: artifact.sha256
            )
            downloadedURL = destination
            previewURL = destination
        } catch {
            operationError = error.localizedDescription
        }
    }

    @MainActor
    private func startAction(_ consumer: RunArtifactConsumer) async {
        guard !isStarting else {
            return
        }
        isStarting = true
        operationError = nil
        defer { isStarting = false }

        do {
            let created = try await client.createRun(
                targetID: run.targetId,
                action: consumer.action,
                parameters: [:],
                inputs: [consumer.input: artifact.id],
                idempotencyKey: idempotencyKey
            )
            idempotencyKey = UUID().uuidString.lowercased()
            onRunChanged()
            createdRun = created
        } catch {
            operationError = error.localizedDescription
        }
    }

    private var artifactDownloadURL: URL {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("slipramp-artifacts", isDirectory: true)
        return directory.appendingPathComponent("\(artifact.id)-\(artifactFilename)")
    }

    private var artifactFilename: String {
        let safeName = artifact.name.map { character in
            character.isLetter || character.isNumber || "-_.".contains(character) ? character : "-"
        }
        var name = String(safeName)
        if URL(fileURLWithPath: name).pathExtension.isEmpty,
           let fileExtension = UTType(mimeType: artifact.mediaType)?.preferredFilenameExtension
        {
            name += ".\(fileExtension)"
        }
        return name
    }

    private func removeDownloadedArtifact() {
        guard let downloadedURL else {
            return
        }
        try? FileManager.default.removeItem(at: downloadedURL)
        self.downloadedURL = nil
    }

    private func display(_ value: String) -> String {
        value.replacingOccurrences(of: "-", with: " ").capitalized
    }
}

private func artifactTone(_ artifact: Artifact) -> SliprampTone {
    guard artifact.retentionState == .retained else {
        return .neutral
    }
    switch artifact.state {
    case .open:
        return .info
    case .sealed:
        return .success
    case .failed:
        return .danger
    }
}

private func artifactSymbol(_ artifact: Artifact) -> String {
    guard artifact.retentionState == .retained else {
        return "archivebox.fill"
    }
    switch artifact.state {
    case .open:
        return "arrow.down.doc.fill"
    case .sealed:
        return "checkmark.circle.fill"
    case .failed:
        return "xmark.octagon.fill"
    }
}

private func formatBytes(_ bytes: Int64) -> String {
    ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
}
