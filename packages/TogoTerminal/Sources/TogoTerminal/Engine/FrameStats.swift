import QuartzCore

/// Frame pacing measured from `CADisplayLink` callbacks.
///
/// A frame counts as dropped when the gap since the previous callback exceeds 1.5 times the
/// display's frame duration. The duration comes from `targetTimestamp - timestamp` when the
/// caller passes it; otherwise the median gap stands in, which holds as long as most frames
/// arrive on time. Only the most recent minute at 120 Hz is kept, so a display link left
/// running does not grow memory.
public struct FrameStats: Sendable {
    private static let capacity = 7_200

    private struct Sample: Sendable {
        var interval: CFTimeInterval
        var expected: CFTimeInterval?
    }

    private var lastTimestamp: CFTimeInterval?
    private var expectedDuration: CFTimeInterval?
    private var samples: [Sample] = []
    private var next = 0

    public init() {}

    public mutating func record(timestamp: CFTimeInterval) {
        append(endingAt: timestamp)
    }

    public mutating func record(timestamp: CFTimeInterval, targetTimestamp: CFTimeInterval) {
        append(endingAt: timestamp)
        expectedDuration = targetTimestamp - timestamp
    }

    public var p95Interval: CFTimeInterval {
        let sorted = samples.map(\.interval).sorted()
        guard !sorted.isEmpty else { return 0 }
        let rank = Int((Double(sorted.count) * 0.95).rounded(.up)) - 1
        return sorted[min(max(rank, 0), sorted.count - 1)]
    }

    public var droppedFraction: Double {
        guard !samples.isEmpty else { return 0 }
        let sorted = samples.map(\.interval).sorted()
        let median = sorted[sorted.count / 2]
        let dropped = samples.count { $0.interval > 1.5 * ($0.expected ?? median) }
        return Double(dropped) / Double(samples.count)
    }

    private mutating func append(endingAt timestamp: CFTimeInterval) {
        defer { lastTimestamp = timestamp }
        guard let last = lastTimestamp else { return }
        let sample = Sample(interval: timestamp - last, expected: expectedDuration)
        if samples.count < Self.capacity {
            samples.append(sample)
        } else {
            samples[next] = sample
            next = (next + 1) % Self.capacity
        }
    }
}
