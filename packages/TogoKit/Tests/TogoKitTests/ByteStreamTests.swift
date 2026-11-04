import Foundation
import Synchronization
import Testing
import TogoKit

struct ByteStreamTests {
    @Test func byteStreamBoundsProducer() async {
        let stream = ByteStream(limit: 1024)
        let chunkCount = 64
        let peak = Mutex(0)

        let producer = Task {
            for index in 0..<chunkCount {
                let accepted = await stream.write(Data(repeating: UInt8(index), count: 1024))
                #expect(accepted)
                peak.withLock { $0 = max($0, stream.bufferedBytes) }
            }
            stream.finish()
        }

        var received: [Data] = []
        for await chunk in stream {
            peak.withLock { $0 = max($0, stream.bufferedBytes) }
            received.append(chunk)
            try? await Task.sleep(for: .milliseconds(1))
        }
        await producer.value

        #expect(received == (0..<chunkCount).map { Data(repeating: UInt8($0), count: 1024) })
        #expect(peak.withLock { $0 } <= 2048)
    }

    @Test func byteStreamFinishReleasesWriters() async {
        let stream = ByteStream(limit: 4)
        #expect(await stream.write(Data("abcd".utf8)))

        let blocked = Task { await stream.write(Data("efgh".utf8)) }
        for _ in 0..<100 { await Task.yield() }
        #expect(stream.bufferedBytes == 4)

        stream.finish()
        #expect(await blocked.value == false)
        #expect(await stream.write(Data("late".utf8)) == false)

        var iterator = stream.makeAsyncIterator()
        #expect(await iterator.next() == Data("abcd".utf8))
        #expect(await iterator.next() == nil)
    }

    @Test func writersResumeInOrder() async {
        let stream = ByteStream(limit: 1)
        #expect(await stream.write(Data([0])))
        var writers: [Task<Bool, Never>] = []
        for value in 1...5 {
            writers.append(Task { await stream.write(Data([UInt8(value)])) })
            // Give each writer time to queue before the next one starts.
            for _ in 0..<100 { await Task.yield() }
        }
        var received: [UInt8] = []
        var iterator = stream.makeAsyncIterator()
        while received.count < 6, let chunk = await iterator.next() {
            received += chunk
        }
        for writer in writers { #expect(await writer.value) }
        #expect(received == [0, 1, 2, 3, 4, 5])
    }

    @Test func waitingConsumerReceivesLaterWrite() async {
        let stream = ByteStream()
        let consumer = Task {
            var iterator = stream.makeAsyncIterator()
            return await iterator.next()
        }
        for _ in 0..<50 { await Task.yield() }
        #expect(await stream.write(Data("hi".utf8)))
        #expect(await consumer.value == Data("hi".utf8))
        #expect(stream.bufferedBytes == 0)
    }

    @Test func finishWakesWaitingConsumer() async {
        let stream = ByteStream()
        let consumer = Task {
            var iterator = stream.makeAsyncIterator()
            return await iterator.next()
        }
        for _ in 0..<50 { await Task.yield() }
        stream.finish()
        #expect(await consumer.value == nil)
    }

    @Test func cancelledWriterReturnsFalse() async {
        let stream = ByteStream(limit: 1)
        #expect(await stream.write(Data([1])))
        let blocked = Task { await stream.write(Data([2])) }
        for _ in 0..<50 { await Task.yield() }
        blocked.cancel()
        #expect(await blocked.value == false)
        stream.finish()

        var received: [Data] = []
        for await chunk in stream { received.append(chunk) }
        #expect(received == [Data([1])])
    }

    /// Races a fast producer against a fast consumer on a tiny limit. A drain that skipped
    /// admitting queued writers once stranded a writer here and hung the run.
    @Test func noLostWakeupUnderContention() async {
        for _ in 0..<200 {
            let stream = ByteStream(limit: 1)
            let producer = Task {
                for value in 0..<100 { _ = await stream.write(Data([UInt8(value)])) }
                stream.finish()
            }
            var received: [UInt8] = []
            for await chunk in stream { received += chunk }
            await producer.value
            #expect(received == (0..<100).map { UInt8($0) })
        }
    }

    @Test func cancelledConsumerReturnsNil() async {
        let stream = ByteStream()
        let consumer = Task {
            var iterator = stream.makeAsyncIterator()
            return await iterator.next()
        }
        for _ in 0..<50 { await Task.yield() }
        consumer.cancel()
        #expect(await consumer.value == nil)
    }
}
