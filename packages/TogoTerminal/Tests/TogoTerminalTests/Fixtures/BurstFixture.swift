import Foundation

/// Output shaped like a coding agent streaming into a full-screen TUI: colour and bold runs,
/// rounded box-drawing frames, CJK and emoji in the text, lines 200 columns wide, cursor moves
/// and synchronized-update brackets. The same seed always gives the same bytes.
enum BurstFixture {
    static let columns = 200

    private static let esc = "\u{1b}"
    private static let words = [
        "let", "value", "await", "func", "return", "self.store", "items", "map", "{", "}", "(", ")", "->",
        "diff", "--git", "+", "-", "src/router.ts", "✓", "✗", "読み込み", "完了", "변경", "テスト", "🙂", "→",
    ]

    /// At least `bytes` bytes of output.
    static func make(bytes: Int, seed: UInt64 = 0x5EED_CAFE_F00D_0001) -> [UInt8] {
        var random = seed
        func next(_ bound: Int) -> Int {
            random = random &* 6_364_136_223_846_793_005 &+ 1_442_695_040_888_963_407
            return Int((random >> 33) % UInt64(bound))
        }
        var out: [UInt8] = []
        out.reserveCapacity(bytes + 16 * 1024)
        var row = 0
        while out.count < bytes {
            var frame = "\(esc)[?2026h\(esc)[H"
            frame += "\(esc)[38;5;245m╭" + String(repeating: "─", count: columns - 2) + "╮\(esc)[0m\r\n"
            for _ in 0..<24 {
                frame += "\(esc)[38;5;245m│\(esc)[0m "
                var used = 2
                while used < columns - 2 {
                    let word = words[next(words.count)]
                    let width = word.unicodeScalars.reduce(0) { $0 + ($1.value >= 0x1100 ? 2 : 1) } + 1
                    guard used + width <= columns - 2 else { break }
                    switch next(5) {
                    case 0: frame += "\(esc)[1;3\(1 + next(6))m\(word)\(esc)[0m "
                    case 1: frame += "\(esc)[38;2;\(next(256));\(next(256));\(next(256))m\(word)\(esc)[39m "
                    case 2: frame += "\(esc)[48;5;\(next(256))m\(word)\(esc)[49m "
                    default: frame += word + " "
                    }
                    used += width
                }
                frame += String(repeating: " ", count: max(0, columns - 1 - used))
                frame += "\(esc)[38;5;245m│\(esc)[0m\r\n"
                row += 1
            }
            frame += "\(esc)[38;5;245m╰" + String(repeating: "─", count: columns - 2) + "╯\(esc)[0m\r\n"
            frame += "\(esc)[\(1 + row % 24);\(1 + row % 80)H\(esc)[K\(esc)[?2026l"
            out.append(contentsOf: frame.utf8)
        }
        return out
    }
}
