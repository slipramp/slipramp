import SwiftUI
import UIKit

@MainActor
private final class RunLogViewModel: ObservableObject {
    enum Status: Equatable {
        case waitingForJob
        case waitingForLogs
        case connecting
        case live
        case reconnecting
        case sealed
        case incomplete
        case unavailable(label: String, message: String, isFailure: Bool)
        case failed(String)
    }

    @Published private(set) var contents = AttributedString()
    @Published private(set) var contentsRevision = 0
    @Published private(set) var status: Status = .connecting
    @Published private(set) var omittedBytes = 0

    private let client: SliprampAPIClient
    private let runID: String
    private let maximumVisibleBytes = 4 * 1024 * 1024
    private let refreshIntervalNanoseconds: UInt64 = 2_000_000_000
    private var visibleData = Data()

    init(client: SliprampAPIClient, runID: String) {
        self.client = client
        self.runID = runID
    }

    func follow() async {
        visibleData.removeAll(keepingCapacity: true)
        contents = AttributedString()
        contentsRevision = 0
        omittedBytes = 0
        status = .connecting

        while !Task.isCancelled {
            let run: Run
            do {
                run = try await client.getRun(id: runID)
            } catch is CancellationError {
                return
            } catch {
                guard shouldRetryConnection(after: error) else {
                    status = .failed(error.localizedDescription)
                    return
                }
                status = .reconnecting
                guard await waitForRefresh() else {
                    return
                }
                continue
            }

            if run.phase == .queued {
                status = .waitingForJob
                guard await waitForRefresh() else {
                    return
                }
                continue
            }

            status = .connecting
            do {
                try await followAvailableLog()
                return
            } catch is CancellationError {
                return
            } catch let SliprampAPIError.http(httpStatus, _, _) where httpStatus == 404 {
                if run.phase == .finished {
                    status = unavailableStatus(for: run)
                    return
                }
                status = .waitingForLogs
            } catch {
                guard shouldRetryConnection(after: error) else {
                    status = .failed(error.localizedDescription)
                    return
                }
                status = .reconnecting
            }

            guard await waitForRefresh() else {
                return
            }
        }
    }

    private func followAvailableLog() async throws {
        for try await update in client.streamRunLog(id: runID) {
            switch update {
            case .connected:
                status = .live
            case let .chunk(data, _):
                await append(data)
                status = .live
            case .reconnecting:
                status = .reconnecting
            case .sealed:
                status = .sealed
            case .failed:
                status = .incomplete
            }
        }
    }

    private func waitForRefresh() async -> Bool {
        do {
            try await Task.sleep(nanoseconds: refreshIntervalNanoseconds)
            return !Task.isCancelled
        } catch {
            return false
        }
    }

    private func shouldRetryConnection(after error: Error) -> Bool {
        guard let apiError = error as? SliprampAPIError else {
            return true
        }

        switch apiError {
        case let .http(status, _, _):
            return status == 408 || status == 429 || status >= 500
        case .invalidResponse:
            return true
        case .invalidServerURL,
             .invalidPagination,
             .compatibilityUnavailable,
             .incompatibleProductAPIContract,
             .apiKeyAuthenticationUnavailable,
             .publicHTTPNotAllowed,
             .trustedPrivateHTTPConfirmationRequired,
             .nativeOIDCAuthenticationUnavailable,
             .nativeOIDCSignInFailed,
             .connectionFailed,
             .decoding,
             .invalidArtifact,
             .invalidLogStream:
            return false
        }
    }

    private func unavailableStatus(for run: Run) -> Status {
        switch run.outcome {
        case .some(.succeeded):
            return .unavailable(
                label: "No log output",
                message: "This Run completed without producing log output.",
                isFailure: false
            )
        case .some(.failed):
            return .unavailable(
                label: "No logs available",
                message: terminalMessage(
                    run,
                    fallback: "The Run failed before a durable log became available."
                ),
                isFailure: true
            )
        case .some(.timedOut):
            return .unavailable(
                label: "No logs available",
                message: terminalMessage(
                    run,
                    fallback: "The Run timed out before a durable log became available."
                ),
                isFailure: true
            )
        case .some(.canceled):
            return .unavailable(
                label: "No logs available",
                message: "The Run was canceled before a durable log became available.",
                isFailure: false
            )
        case .none:
            return .unavailable(
                label: "No logs available",
                message: "The Run finished before a durable log became available.",
                isFailure: true
            )
        }
    }

    private func terminalMessage(_ run: Run, fallback: String) -> String {
        guard let outcomeMessage = run.outcomeMessage else {
            return fallback
        }
        return "\(outcomeMessage) No durable log is available."
    }

    private func append(_ data: Data) async {
        visibleData.append(data)
        if visibleData.count > maximumVisibleBytes {
            let overflow = visibleData.count - maximumVisibleBytes
            visibleData.removeFirst(overflow)
            omittedBytes += overflow
        }

        let snapshot = visibleData
        let segments = await Task.detached(priority: .userInitiated) {
            let text = String(decoding: snapshot, as: UTF8.self)
            return ANSITextParser.parse(text)
        }.value
        guard !Task.isCancelled else {
            return
        }

        contents = attributedText(for: segments)
        contentsRevision += 1
    }
}

@MainActor
private func attributedText(for segments: [ANSITextSegment]) -> AttributedString {
    let result = NSMutableAttributedString(string: "")
    let pointSize = UIFont.preferredFont(forTextStyle: .caption1).pointSize

    for segment in segments {
        let opacity = segment.style.isDim ? 0.65 : 1
        let foreground = segment.style.foregroundColor ?? .defaultForeground
        var attributes: [NSAttributedString.Key: Any] = [
            .foregroundColor: segment.style.isHidden
                ? UIColor.clear
                : uiColor(foreground, opacity: opacity),
            .font: ansiFont(for: segment.style, pointSize: pointSize),
        ]

        if let background = segment.style.backgroundColor {
            attributes[.backgroundColor] = segment.style.isHidden
                ? UIColor.clear
                : uiColor(background, opacity: opacity)
        }
        if segment.style.isStrikethrough {
            attributes[.strikethroughStyle] = NSUnderlineStyle.single.rawValue
        }
        if segment.style.isUnderlined {
            attributes[.underlineStyle] = NSUnderlineStyle.single.rawValue
        }

        result.append(NSAttributedString(string: segment.text, attributes: attributes))
    }

    return AttributedString(result)
}

@MainActor
private func ansiFont(for style: ANSITextStyle, pointSize: CGFloat) -> UIFont {
    let weight: UIFont.Weight = style.isBold ? .bold : .regular
    let font = UIFont.monospacedSystemFont(ofSize: pointSize, weight: weight)
    guard style.isItalic,
          let descriptor = font.fontDescriptor.withSymbolicTraits(.traitItalic) else {
        return font
    }

    return UIFont(descriptor: descriptor, size: pointSize)
}

@MainActor
private func uiColor(_ color: ANSITextColor, opacity: Double) -> UIColor {
    UIColor(
        red: CGFloat(color.red) / 255,
        green: CGFloat(color.green) / 255,
        blue: CGFloat(color.blue) / 255,
        alpha: CGFloat(opacity)
    )
}

struct RunLogView: View {
    @StateObject private var model: RunLogViewModel

    init(client: SliprampAPIClient, runID: String) {
        _model = StateObject(wrappedValue: RunLogViewModel(client: client, runID: runID))
    }

    var body: some View {
        VStack(spacing: 0) {
            statusBar
            Divider()

            ScrollViewReader { proxy in
                GeometryReader { geometry in
                    ScrollView([.horizontal, .vertical]) {
                        VStack(alignment: .leading, spacing: 8) {
                            if model.omittedBytes > 0 {
                                Text("… \(model.omittedBytes) earlier bytes omitted …")
                                    .foregroundStyle(SliprampTone.warning.color)
                            }

                            if model.contents.characters.isEmpty {
                                Text(emptyMessage)
                                    .foregroundStyle(Color.white.opacity(0.72))
                                    .frame(
                                        width: max(geometry.size.width - 24, 0),
                                        alignment: .leading
                                    )
                                    .fixedSize(horizontal: false, vertical: true)
                                    .textSelection(.enabled)
                            } else {
                                Text(model.contents)
                                    .textSelection(.enabled)
                            }

                            Color.clear
                                .frame(width: 1, height: 1)
                                .id("log-bottom")
                        }
                        .font(.system(.caption, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(12)
                    }
                    .background(SliprampTheme.logBackground)
                    .onChange(of: model.contentsRevision) {
                        withAnimation(.easeOut(duration: 0.1)) {
                            proxy.scrollTo("log-bottom", anchor: .bottom)
                        }
                    }
                }
            }
        }
        .navigationTitle("Run log")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await model.follow()
        }
    }

    @ViewBuilder
    private var statusBar: some View {
        HStack(spacing: 8) {
            Image(systemName: statusSymbol)
                .foregroundStyle(statusTone.color)
            Text(statusLabel)
                .font(.caption.weight(.semibold))
            Spacer()
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .background(.bar)
    }

    private var statusLabel: String {
        switch model.status {
        case .waitingForJob: "Waiting for job"
        case .waitingForLogs: "Waiting for logs"
        case .connecting: "Connecting"
        case .live: "Live"
        case .reconnecting: "Reconnecting"
        case .sealed: "Complete"
        case .incomplete: "Incomplete log"
        case let .unavailable(label, _, _): label
        case let .failed(message): message
        }
    }

    private var statusSymbol: String {
        switch model.status {
        case .waitingForJob, .waitingForLogs: "clock.fill"
        case .connecting, .reconnecting: "arrow.triangle.2.circlepath"
        case .live: "dot.radiowaves.left.and.right"
        case .sealed: "checkmark.circle.fill"
        case .incomplete, .unavailable, .failed: "exclamationmark.triangle.fill"
        }
    }

    private var statusTone: SliprampTone {
        switch model.status {
        case .waitingForJob: .warning
        case .waitingForLogs: .info
        case .connecting, .reconnecting: .warning
        case .live: .accent
        case .sealed: .success
        case .incomplete, .failed: .danger
        case let .unavailable(_, _, isFailure): isFailure ? .danger : .neutral
        }
    }

    private var emptyMessage: String {
        switch model.status {
        case .waitingForJob: "This Run has not started yet. Logs will appear after the execution Job begins."
        case .waitingForLogs: "The Job is starting. Logs will stream here when they become available."
        case .connecting: "Waiting for the durable Run log…"
        case .reconnecting: "The stream disconnected. Reconnecting…"
        case .sealed: "This Run completed without log output."
        case .incomplete: "Log collection failed before any bytes were retained."
        case let .unavailable(_, message, _): message
        case let .failed(message): message
        case .live: "Waiting for output…"
        }
    }
}
