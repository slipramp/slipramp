import SwiftUI

@MainActor
private final class RunLogViewModel: ObservableObject {
    enum Status: Equatable {
        case connecting
        case live
        case reconnecting
        case sealed
        case incomplete
        case failed(String)
    }

    @Published private(set) var contents = ""
    @Published private(set) var status: Status = .connecting
    @Published private(set) var omittedBytes = 0

    private let client: SliprampAPIClient
    private let runID: String
    private let maximumVisibleBytes = 4 * 1024 * 1024
    private var visibleData = Data()

    init(client: SliprampAPIClient, runID: String) {
        self.client = client
        self.runID = runID
    }

    func follow() async {
        visibleData.removeAll(keepingCapacity: true)
        contents = ""
        omittedBytes = 0
        status = .connecting

        do {
            for try await update in client.streamRunLog(id: runID) {
                switch update {
                case let .chunk(data, _):
                    append(data)
                    status = .live
                case .reconnecting:
                    status = .reconnecting
                case .sealed:
                    status = .sealed
                case .failed:
                    status = .incomplete
                }
            }
        } catch is CancellationError {
            return
        } catch {
            status = .failed(error.localizedDescription)
        }
    }

    private func append(_ data: Data) {
        visibleData.append(data)
        if visibleData.count > maximumVisibleBytes {
            let overflow = visibleData.count - maximumVisibleBytes
            visibleData.removeFirst(overflow)
            omittedBytes += overflow
        }
        contents = String(decoding: visibleData, as: UTF8.self)
    }
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
                ScrollView([.horizontal, .vertical]) {
                    VStack(alignment: .leading, spacing: 8) {
                        if model.omittedBytes > 0 {
                            Text("… \(model.omittedBytes) earlier bytes omitted …")
                                .foregroundStyle(SliprampTone.warning.color)
                        }

                        Text(model.contents.isEmpty ? emptyMessage : model.contents)
                            .foregroundStyle(
                                model.contents.isEmpty ? Color.secondary : Color.white
                            )
                            .textSelection(.enabled)

                        Color.clear
                            .frame(width: 1, height: 1)
                            .id("log-bottom")
                    }
                    .font(.system(.caption, design: .monospaced))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12)
                }
                .background(SliprampTheme.logBackground)
                .onChange(of: model.contents) {
                    withAnimation(.easeOut(duration: 0.1)) {
                        proxy.scrollTo("log-bottom", anchor: .bottom)
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
        case .connecting: "Connecting"
        case .live: "Live"
        case .reconnecting: "Reconnecting"
        case .sealed: "Complete"
        case .incomplete: "Incomplete log"
        case let .failed(message): message
        }
    }

    private var statusSymbol: String {
        switch model.status {
        case .connecting, .reconnecting: "arrow.triangle.2.circlepath"
        case .live: "dot.radiowaves.left.and.right"
        case .sealed: "checkmark.circle.fill"
        case .incomplete, .failed: "exclamationmark.triangle.fill"
        }
    }

    private var statusTone: SliprampTone {
        switch model.status {
        case .connecting, .reconnecting: .warning
        case .live: .accent
        case .sealed: .success
        case .incomplete, .failed: .danger
        }
    }

    private var emptyMessage: String {
        switch model.status {
        case .connecting: "Waiting for the durable Run log…"
        case .reconnecting: "The stream disconnected. Reconnecting…"
        case .sealed: "This Run completed without log output."
        case .incomplete: "Log collection failed before any bytes were retained."
        case let .failed(message): message
        case .live: "Waiting for output…"
        }
    }
}
