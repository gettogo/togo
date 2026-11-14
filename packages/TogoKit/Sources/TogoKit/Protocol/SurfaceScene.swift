import Foundation

/// A TSP v1 node. Properties are values, never executable markup.
public struct SurfaceNode: Decodable, Hashable, Sendable, Identifiable {
    public var id: String
    public var kind: String
    public var properties: [String: JSONValue]
    public var children: [SurfaceNode]

    private enum CodingKeys: String, CodingKey { case id, kind = "k", properties = "p", children = "c" }

    public init(from decoder: any Decoder) throws {
        // A child adds both an array index and a `c` key. Stop before recursive
        // decoding allocates an arbitrarily deep tree, then validate exact depth.
        guard decoder.codingPath.count <= 133 else { throw SurfaceProtocolError.invalidSnapshot }
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decode(String.self, forKey: .kind)
        properties = try c.decodeIfPresent([String: JSONValue].self, forKey: .properties) ?? [:]
        children = try c.decodeIfPresent([SurfaceNode].self, forKey: .children) ?? []
    }

    public func node(id: String) -> SurfaceNode? {
        if self.id == id { return self }
        for child in children {
            if let found = child.node(id: id) { return found }
        }
        return nil
    }
}

public struct SurfaceDocument: Decodable, Hashable, Sendable {
    public var root: SurfaceNode
    public var sequence: Int64
    public var revision: UInt64
    public var focus: String?
    public var suspended: Bool
}

public struct SurfaceView: Decodable, Hashable, Sendable, Identifiable {
    public enum Mode: String, Decodable, Sendable { case inline, screen, flow }
    public var id: String
    public var mode: Mode
    public var title: String?
    public var role: String?
    public var listen: Bool
    public var closed: Bool
    public var instance: UInt64
    public var document: SurfaceDocument
}

/// Published atomically only after every chunk and node has been validated.
public struct SurfaceScene: Decodable, Hashable, Sendable {
    public var program: String
    public var features: [String]
    public var views: [SurfaceView]
    public var revision: UInt64
    public var options: SurfaceOptions

    public static func decode(_ data: Data, kinds: Set<String>) throws -> Self {
        guard data.count <= SurfaceSnapshotAssembler.maxBytes, boundedJSON(data) else {
            throw SurfaceProtocolError.invalidSnapshot
        }
        let scene = try JSONDecoder().decode(Self.self, from: data)
        guard scene.views.count <= 4, scene.program.utf8.count <= 128,
              scene.features.count <= 3, Set(scene.features).isSubset(of: ["edit", "undo", "send"]),
              !scene.options.kinds.isEmpty, scene.options.kinds.count <= 64,
              scene.options.cols > 0, scene.options.cols <= 1000 else { throw SurfaceProtocolError.invalidSnapshot }
        var surfaces = Set<String>()
        let negotiated = Set(scene.options.kinds)
        guard negotiated.isSubset(of: kinds) else { throw SurfaceProtocolError.unsupportedContent }
        var instances = Set<UInt64>()
        for view in scene.views {
            guard validID(view.id), surfaces.insert(view.id).inserted, view.instance > 0,
                  instances.insert(view.instance).inserted, view.document.root.id == view.id,
                  view.document.root.kind == "col", view.document.sequence >= 0,
                  (view.title?.utf8.count ?? 0) <= 4096, (view.role?.utf8.count ?? 0) <= 256 else {
                throw SurfaceProtocolError.invalidSnapshot
            }
            var ids = Set<String>()
            var stack = [(view.document.root, 0)]
            while let (node, depth) = stack.popLast() {
                guard depth <= 64, ids.count < 16_384, validID(node.id), ids.insert(node.id).inserted else {
                    throw SurfaceProtocolError.invalidSnapshot
                }
                guard negotiated.contains(node.kind) else { throw SurfaceProtocolError.unsupportedContent }
                for child in node.children { stack.append((child, depth + 1)) }
            }
            if let focus = view.document.focus, !ids.contains(focus) { throw SurfaceProtocolError.invalidSnapshot }
        }
        return scene
    }

    private static func validID(_ value: String) -> Bool {
        !value.isEmpty && value.utf8.count <= 4096 && !value.unicodeScalars.contains { CharacterSet.controlCharacters.contains($0) }
    }

    private static func boundedJSON(_ data: Data) -> Bool {
        guard String(data: data, encoding: .utf8) != nil else { return false }
        var depth = 0, quoted = false, escaped = false
        for byte in data {
            if quoted {
                if escaped { escaped = false }
                else if byte == 92 { escaped = true }
                else if byte == 34 { quoted = false }
            } else if byte == 34 { quoted = true }
            else if byte == 91 || byte == 123 {
                depth += 1
                if depth > 256 { return false }
            } else if byte == 93 || byte == 125 { depth -= 1 }
        }
        return true
    }
}
