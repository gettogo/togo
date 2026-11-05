/// File names in git diff headers. Git wraps a name in double quotes and C escapes when it
/// holds control characters, quotes or backslashes (and, without `core.quotepath=off`,
/// non-ASCII bytes as octal).
enum DiffPaths {
    /// The two names after `diff --git `, prefixes removed.
    static func gitHeader(_ rest: Substring) -> (old: String, new: String)? {
        let bytes = Array(rest.utf8)
        var old: [UInt8]
        var new: [UInt8]
        if bytes.first == UInt8(ascii: "\"") {
            guard let first = unquote(bytes[...]) else { return nil }
            old = first.value
            var tail = first.rest
            while tail.first == UInt8(ascii: " ") { tail = tail.dropFirst() }
            if tail.first == UInt8(ascii: "\"") {
                guard let second = unquote(tail) else { return nil }
                new = second.value
            } else {
                new = Array(tail)
            }
        } else if let split = lastIndex(of: Array(" \"".utf8), in: bytes) {
            old = Array(bytes[..<split])
            guard let second = unquote(bytes[(split + 1)...]) else { return nil }
            new = second.value
        } else if let split = sameNameSplit(bytes) ?? lastIndex(of: Array(" b/".utf8), in: bytes) {
            old = Array(bytes[..<split])
            new = Array(bytes[(split + 1)...])
        } else {
            return nil
        }
        old = strip(old, prefix: "a/")
        new = strip(new, prefix: "b/")
        return (decode(old), decode(new))
    }

    /// A `---` or `+++` name; nil for `/dev/null`. Git adds a tab after names with spaces.
    static func side(_ text: Substring, prefix: String) -> String? {
        var bytes = Array(text.utf8)[...]
        if bytes.first == UInt8(ascii: "\""), let quoted = unquote(bytes) {
            bytes = quoted.value[...]
        } else if let tab = bytes.firstIndex(of: 0x09) {
            bytes = bytes[..<tab]
        }
        if bytes.elementsEqual("/dev/null".utf8) { return nil }
        return decode(strip(Array(bytes), prefix: prefix))
    }

    /// A name in `rename from`, `copy to` and similar lines: quoted or literal, no prefix.
    static func plain(_ text: Substring) -> String {
        let bytes = Array(text.utf8)
        if bytes.first == UInt8(ascii: "\""), let quoted = unquote(bytes[...]) {
            return decode(quoted.value)
        }
        return decode(bytes)
    }

    /// Reads a C-quoted string at the start of `bytes`. Returns the unescaped bytes and what
    /// follows the closing quote, or nil when the quote never closes.
    static func unquote(_ bytes: ArraySlice<UInt8>) -> (value: [UInt8], rest: ArraySlice<UInt8>)? {
        guard bytes.first == UInt8(ascii: "\"") else { return nil }
        var value: [UInt8] = []
        var index = bytes.startIndex + 1
        while index < bytes.endIndex {
            let byte = bytes[index]
            if byte == UInt8(ascii: "\"") { return (value, bytes[(index + 1)...]) }
            guard byte == UInt8(ascii: "\\"), index + 1 < bytes.endIndex else {
                value.append(byte)
                index += 1
                continue
            }
            let code = bytes[index + 1]
            index += 2
            switch code {
            case UInt8(ascii: "a"): value.append(0x07)
            case UInt8(ascii: "b"): value.append(0x08)
            case UInt8(ascii: "t"): value.append(0x09)
            case UInt8(ascii: "n"): value.append(0x0A)
            case UInt8(ascii: "v"): value.append(0x0B)
            case UInt8(ascii: "f"): value.append(0x0C)
            case UInt8(ascii: "r"): value.append(0x0D)
            case UInt8(ascii: "0")...UInt8(ascii: "7"):
                var octal = Int(code - 0x30)
                var digits = 1
                while digits < 3, index < bytes.endIndex, (0x30...0x37).contains(bytes[index]) {
                    octal = octal * 8 + Int(bytes[index] - 0x30)
                    index += 1
                    digits += 1
                }
                value.append(UInt8(truncatingIfNeeded: octal))
            default: value.append(code)
            }
        }
        return nil
    }

    /// For `a/X b/X`, the space between two equal names, which may themselves hold spaces.
    private static func sameNameSplit(_ bytes: [UInt8]) -> Int? {
        guard bytes.count >= 5, bytes.count % 2 == 1 else { return nil }
        let middle = bytes.count / 2
        guard bytes[middle] == UInt8(ascii: " ") else { return nil }
        let left = bytes[..<middle], right = bytes[(middle + 1)...]
        guard left.dropFirst(2).elementsEqual(right.dropFirst(2)) else { return nil }
        return middle
    }

    private static func lastIndex(of needle: [UInt8], in bytes: [UInt8]) -> Int? {
        guard bytes.count >= needle.count else { return nil }
        for start in stride(from: bytes.count - needle.count, through: 0, by: -1)
        where bytes[start..<(start + needle.count)].elementsEqual(needle) {
            return start
        }
        return nil
    }

    private static func strip(_ bytes: [UInt8], prefix: String) -> [UInt8] {
        bytes.starts(with: prefix.utf8) ? Array(bytes.dropFirst(prefix.utf8.count)) : bytes
    }

    private static func decode(_ bytes: [UInt8]) -> String {
        String(decoding: bytes, as: UTF8.self)
    }
}
