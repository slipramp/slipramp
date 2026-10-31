import Foundation

enum RunLogStreamUpdate: Equatable, Sendable {
    case connected(offset: Int64)
    case chunk(Data, nextOffset: Int64)
    case reconnecting(offset: Int64)
    case sealed(offset: Int64)
    case failed(offset: Int64)
}

struct RunLogSSEParser {
    private static let maximumLineBytes = 16 << 20

    private(set) var offset: Int64

    private var eventName = ""
    private var eventID = ""
    private var dataLines: [String] = []
    private var lineBytes = Data()

    init(offset: Int64 = 0) {
        self.offset = offset
    }

    mutating func consume(byte: UInt8) throws -> RunLogStreamUpdate? {
        guard byte == 0x0A else {
            guard lineBytes.count < Self.maximumLineBytes else {
                throw SliprampAPIError.invalidLogStream(
                    "An SSE line exceeds the supported size."
                )
            }
            lineBytes.append(byte)
            return nil
        }

        if lineBytes.last == 0x0D {
            lineBytes.removeLast()
        }
        guard let line = String(data: lineBytes, encoding: .utf8) else {
            throw SliprampAPIError.invalidLogStream("An SSE line is not valid UTF-8.")
        }
        lineBytes.removeAll(keepingCapacity: true)
        return try consume(line: line)
    }

    mutating func consume(line rawLine: String) throws -> RunLogStreamUpdate? {
        guard rawLine.utf8.count <= Self.maximumLineBytes else {
            throw SliprampAPIError.invalidLogStream("An SSE line exceeds the supported size.")
        }

        let line = rawLine.hasSuffix("\r") ? String(rawLine.dropLast()) : rawLine
        if line.isEmpty {
            defer { resetEvent() }
            guard !eventName.isEmpty || !eventID.isEmpty || !dataLines.isEmpty else {
                return nil
            }
            return try dispatchEvent()
        }

        guard !line.hasPrefix(":") else {
            return nil
        }

        let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let field = String(parts[0])
        var value = parts.count == 2 ? String(parts[1]) : ""
        if value.hasPrefix(" ") {
            value.removeFirst()
        }

        switch field {
        case "event":
            eventName = value
        case "id":
            eventID = value
        case "data":
            dataLines.append(value)
        default:
            break
        }
        return nil
    }

    private mutating func dispatchEvent() throws -> RunLogStreamUpdate {
        guard let parsedID = Int64(eventID), parsedID >= 0 else {
            throw SliprampAPIError.invalidLogStream(
                "An SSE event ID is not a nonnegative byte offset."
            )
        }

        switch eventName.isEmpty ? "message" : eventName {
        case "chunk":
            let data = Data(dataLines.joined(separator: "\n").utf8)
            let payload: RunLogChunkPayload
            do {
                payload = try JSONDecoder().decode(RunLogChunkPayload.self, from: data)
            } catch {
                throw SliprampAPIError.invalidLogStream(
                    "A chunk event could not be decoded: \(error.localizedDescription)"
                )
            }

            guard let contents = Data(base64Encoded: payload.contentsBase64),
                  payload.offsetBytes >= 0,
                  payload.nextOffsetBytes >= payload.offsetBytes,
                  payload.nextOffsetBytes - payload.offsetBytes == Int64(contents.count),
                  parsedID == payload.nextOffsetBytes
            else {
                throw SliprampAPIError.invalidLogStream(
                    "A chunk event contains inconsistent byte offsets."
                )
            }
            guard payload.offsetBytes <= offset, payload.nextOffsetBytes >= offset else {
                throw SliprampAPIError.invalidLogStream(
                    "A chunk event does not continue from durable byte offset \(offset)."
                )
            }

            let overlap = Int(offset - payload.offsetBytes)
            let newContents = contents.dropFirst(overlap)
            offset = payload.nextOffsetBytes
            return .chunk(Data(newContents), nextOffset: offset)

        case "sealed":
            guard parsedID == offset else {
                throw SliprampAPIError.invalidLogStream(
                    "A sealed event does not match the received log bytes."
                )
            }
            return .sealed(offset: offset)

        case "failed":
            guard parsedID == offset else {
                throw SliprampAPIError.invalidLogStream(
                    "A failed event does not match the received log bytes."
                )
            }
            return .failed(offset: offset)

        default:
            throw SliprampAPIError.invalidLogStream(
                "The Server sent an unsupported SSE event named \(eventName)."
            )
        }
    }

    private mutating func resetEvent() {
        eventName = ""
        eventID = ""
        dataLines.removeAll(keepingCapacity: true)
    }
}

private struct RunLogChunkPayload: Decodable {
    let offsetBytes: Int64
    let nextOffsetBytes: Int64
    let contentsBase64: String
}
