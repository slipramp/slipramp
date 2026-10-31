import SwiftUI

struct ActionDetailView: View {
    @State private var parameterValues: [String: String] = [:]
    @State private var operationError: String?
    @State private var isSubmitting = false
    @State private var showStartConfirmation = false
    @State private var createdRun: Run?
    @State private var idempotencyKey = UUID().uuidString.lowercased()

    private let client: SliprampAPIClient
    private let target: CatalogTarget
    private let actionName: String
    private let action: TargetAction
    private let onRunChanged: @MainActor () -> Void

    init(
        client: SliprampAPIClient,
        target: CatalogTarget,
        actionName: String,
        action: TargetAction,
        onRunChanged: @escaping @MainActor () -> Void = {}
    ) {
        self.client = client
        self.target = target
        self.actionName = actionName
        self.action = action
        self.onRunChanged = onRunChanged
    }

    var body: some View {
        List {
            overviewSection
            executionSection
            parametersSection
            artifactsSection
            startSection
        }
        .scrollContentBackground(.hidden)
        .background(SliprampTheme.canvas)
        .navigationTitle(action.title ?? actionName)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if isSubmitting {
                ToolbarItem(placement: .topBarTrailing) {
                    ProgressView()
                }
            }
        }
        .navigationDestination(item: $createdRun) { run in
            RunDetailView(
                client: client,
                runID: run.id,
                initialRun: run,
                onRunChanged: onRunChanged
            )
        }
    }

    @ViewBuilder
    private var overviewSection: some View {
        Section("Action") {
            if let description = action.description {
                Text(description)
                    .foregroundStyle(.secondary)
            } else {
                Text("Create an immutable execution of this Action.")
                    .foregroundStyle(.secondary)
            }

            if target.archivedAt != nil {
                SliprampCallout(
                    message: "This Action belongs to an archived Target and cannot start a new Run.",
                    tone: .neutral,
                    symbol: "archivebox.fill"
                )
            } else if requiresArtifactInputs {
                SliprampCallout(
                    message: "This Action requires a compatible Artifact input. "
                        + "Start it from a producing Run so the commit and catalog revision stay pinned.",
                    tone: .info,
                    symbol: "shippingbox.fill"
                )
            }

            if let intent = action.intent {
                LabeledContent("Intent", value: display(intent))
            }

            if let value = action.documentationUrl, let url = URL(string: value) {
                Link(destination: url) {
                    Label("Action documentation", systemImage: "arrow.up.right.square")
                        .labelStyle(.titleAndIcon)
                }
            }
        }
    }

    private var executionSection: some View {
        Section("Execution") {
            LabeledContent("Target", value: target.displayName)
            if action.title != nil {
                LabeledContent("Action") {
                    Text(actionName)
                        .fontDesign(.monospaced)
                }
            }
            LabeledContent("Repository", value: target.repositoryName)

            if let runtime = action.runtime ?? target.definition.spec.runtime {
                LabeledContent("Runtime", value: runtime)
            }
            if let placement = action.placement ?? target.definition.spec.placement {
                LabeledContent("Placement", value: placement)
            }
            if let timeout = action.timeout ?? target.definition.spec.timeout {
                LabeledContent("Timeout", value: timeout)
            }

            VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
                Text("Command")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                Text(action.command.joined(separator: " "))
                    .font(.subheadline.monospaced())
                    .textSelection(.enabled)
            }
            .padding(.vertical, SliprampTheme.Space.xSmall)
        }
    }

    @ViewBuilder
    private var parametersSection: some View {
        let parameters = action.parameters?.additionalProperties ?? [:]

        Section("Parameters") {
            if parameters.isEmpty {
                Text("This Action has no parameters.")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(parameters.keys.sorted(), id: \.self) { name in
                    if let parameter = parameters[name] {
                        parameterControl(name: name, parameter: parameter)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private var artifactsSection: some View {
        let inputs = action.inputs?.additionalProperties ?? [:]
        let outputs = action.artifacts?.additionalProperties ?? [:]

        if !inputs.isEmpty {
            Section("Artifact inputs") {
                ForEach(inputs.keys.sorted(), id: \.self) { name in
                    if let input = inputs[name] {
                        let producerName = input.artifact.fromAction
                        if let producer = target.definition.spec.actions.additionalProperties[producerName] {
                            NavigationLink {
                                ActionDetailView(
                                    client: client,
                                    target: target,
                                    actionName: producerName,
                                    action: producer,
                                    onRunChanged: onRunChanged
                                )
                            } label: {
                                artifactInputLabel(name: name, producerName: producerName)
                            }
                        } else {
                            artifactInputLabel(name: name, producerName: producerName)
                        }
                    }
                }
            }
        }

        if !outputs.isEmpty {
            Section("Artifact outputs") {
                ForEach(outputs.keys.sorted(), id: \.self) { name in
                    if let output = outputs[name] {
                        VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
                            Text(name)
                                .font(.headline)
                            if let description = output.description {
                                Text(description)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                            }
                            if let mediaType = output.mediaType {
                                Text(mediaType)
                                    .font(.caption.monospaced())
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
        }
    }

    private func artifactInputLabel(name: String, producerName: String) -> some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.xSmall) {
            Text(name)
                .font(.headline)
            Text("From Action \(producerName)")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    @ViewBuilder
    private var startSection: some View {
        if canStart {
            Section {
                if let operationError {
                    SliprampCallout(message: operationError, tone: .danger)
                }

                Button {
                    validateForConfirmation()
                } label: {
                    Label("Start Action", systemImage: "play.fill")
                        .labelStyle(.titleAndIcon)
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .tint(SliprampTheme.accent)
                .controlSize(.large)
                .disabled(isSubmitting)
                .confirmationDialog(
                    "Start \(action.title ?? actionName)?",
                    isPresented: $showStartConfirmation,
                    titleVisibility: .visible
                ) {
                    Button("Start Run") {
                        Task { await startRun() }
                    }
                    Button("Cancel", role: .cancel) {}
                } message: {
                    Text(
                        "This creates an immutable Run from catalog commit "
                            + "\(target.catalogCommit.shortSHA)."
                    )
                }
            } footer: {
                Text("The Run uses the active catalog revision and begins queued for its configured Placement.")
            }
        }
    }

    @ViewBuilder
    private func parameterControl(name: String, parameter: ActionParameter) -> some View {
        let label = parameter.title ?? name

        VStack(alignment: .leading, spacing: SliprampTheme.Space.small) {
            HStack(alignment: .firstTextBaseline) {
                Text(label)
                    .font(.subheadline.weight(.semibold))
                if parameter.required == true {
                    Text("Required")
                        .font(.caption)
                        .foregroundStyle(SliprampTone.warning.color)
                }
            }

            if parameter.title != nil {
                Text(name)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }

            switch parameter._type {
            case .string:
                TextField("Value", text: binding(for: name))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            case .number:
                TextField("Number", text: binding(for: name))
                    .keyboardType(.numbersAndPunctuation)
            case .boolean:
                Picker("Value", selection: binding(for: name)) {
                    Text(parameter.required == true ? "Select a value" : "Not set").tag("")
                    Text("True").tag("true")
                    Text("False").tag("false")
                }
            case ._enum:
                Picker("Value", selection: binding(for: name)) {
                    Text(parameter.required == true ? "Select a value" : "Not set").tag("")
                    ForEach(parameter.values ?? [], id: \.self) { value in
                        Text(value).tag(value)
                    }
                }
            }

            if let description = parameter.description {
                Text(description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, SliprampTheme.Space.xSmall)
    }

    private var parameterDeclarations: [String: ActionParameter] {
        action.parameters?.additionalProperties ?? [:]
    }

    private var requiresArtifactInputs: Bool {
        !(action.inputs?.additionalProperties.isEmpty ?? true)
    }

    private var canStart: Bool {
        target.archivedAt == nil && !requiresArtifactInputs
    }

    private func binding(for name: String) -> Binding<String> {
        Binding(
            get: { parameterValues[name] ?? "" },
            set: { value in
                parameterValues[name] = value
                operationError = nil
            }
        )
    }

    private func validateForConfirmation() {
        do {
            _ = try ActionParameterDecoder.decode(
                values: parameterValues,
                declarations: parameterDeclarations
            )
            operationError = nil
            showStartConfirmation = true
        } catch {
            operationError = error.localizedDescription
        }
    }

    @MainActor
    private func startRun() async {
        guard !isSubmitting else {
            return
        }

        do {
            let parameters = try ActionParameterDecoder.decode(
                values: parameterValues,
                declarations: parameterDeclarations
            )

            isSubmitting = true
            operationError = nil
            defer { isSubmitting = false }

            let run = try await client.createRun(
                targetID: target.id,
                action: actionName,
                parameters: parameters,
                idempotencyKey: idempotencyKey
            )

            idempotencyKey = UUID().uuidString.lowercased()
            onRunChanged()
            createdRun = run
        } catch {
            operationError = error.localizedDescription
        }
    }

    private func display(_ value: String) -> String {
        value.replacingOccurrences(of: "-", with: " ").capitalized
    }
}
