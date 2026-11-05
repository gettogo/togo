import Foundation

/// The scanning table for one language. Built once per language.
final class LanguageRules: Sendable {
    let keywords: Set<String>
    let lineComment: [String]
    let blockComment: (open: String, close: String)?
    let stringDelimiters: [String]
    let multiLineStrings: [String]
    let typeHeuristic: Bool
    /// `@name` is an attribute.
    let atAttributes: Bool
    /// `#name` is a directive.
    let hashDirectives: Bool
    /// A string followed by `:` is a key.
    let jsonKeys: Bool
    let caseInsensitive: Bool
    let markdown: Bool
    let markup: Bool
    /// A `'` may start a lifetime rather than a character literal.
    let lifetimes: Bool

    let lineCommentUnits: [[UInt16]]
    let blockOpen: [UInt16]
    let blockClose: [UInt16]
    let multiLineUnits: [[UInt16]]
    let quoteUnits: [UInt16]
    /// Hashes of the keywords, so most identifiers are rejected without building a string.
    let keywordHashes: Set<UInt64>
    /// ASCII characters that can begin a comment, string or tag; everything else skips those checks.
    let startsSpecial: [Bool]

    static let hashSeed: UInt64 = 14_695_981_039_346_656_037

    @inline(__always)
    static func mix(_ hash: UInt64, _ unit: UInt16, fold: Bool) -> UInt64 {
        let folded = fold && unit >= 0x41 && unit <= 0x5A ? unit + 0x20 : unit
        return (hash ^ UInt64(folded)) &* 1_099_511_628_211
    }

    init(
        keywords: Set<String> = [],
        lineComment: [String] = [],
        blockComment: (open: String, close: String)? = nil,
        stringDelimiters: [String] = [],
        multiLineStrings: [String] = [],
        typeHeuristic: Bool = false,
        atAttributes: Bool = false,
        hashDirectives: Bool = false,
        jsonKeys: Bool = false,
        caseInsensitive: Bool = false,
        markdown: Bool = false,
        markup: Bool = false,
        lifetimes: Bool = false
    ) {
        self.keywords = keywords
        self.lineComment = lineComment
        self.blockComment = blockComment
        self.stringDelimiters = stringDelimiters
        self.multiLineStrings = multiLineStrings
        self.typeHeuristic = typeHeuristic
        self.atAttributes = atAttributes
        self.hashDirectives = hashDirectives
        self.jsonKeys = jsonKeys
        self.caseInsensitive = caseInsensitive
        self.markdown = markdown
        self.markup = markup
        self.lifetimes = lifetimes
        lineCommentUnits = lineComment.map { Array($0.utf16) }
        blockOpen = Array((blockComment?.open ?? "").utf16)
        blockClose = Array((blockComment?.close ?? "").utf16)
        multiLineUnits = multiLineStrings.map { Array($0.utf16) }
        quoteUnits = stringDelimiters.compactMap { $0.utf16.first }

        var special = [Bool](repeating: false, count: 128)
        let starts = (lineComment + multiLineStrings + stringDelimiters + [blockComment?.open ?? ""])
            .compactMap { $0.utf16.first }
        for unit in starts where unit < 128 { special[Int(unit)] = true }
        if markup {
            special[0x3C] = true
            special[0x3E] = true
        }
        startsSpecial = special
        keywordHashes = Set(keywords.map { word in
            word.utf16.reduce(LanguageRules.hashSeed) { LanguageRules.mix($0, $1, fold: caseInsensitive) }
        })
    }

    static func rules(for language: Language) -> LanguageRules {
        switch language {
        case .swift: swift
        case .go: go
        case .typescript: typescript
        case .javascript: javascript
        case .python: python
        case .rust: rust
        case .ruby: ruby
        case .shell: shell
        case .json: json
        case .yaml: yaml
        case .toml: toml
        case .markdown: markdown
        case .html: html
        case .css: css
        case .c: c
        case .cpp: cpp
        case .objc: objc
        case .java: java
        case .kotlin: kotlin
        case .sql: sql
        case .plain: plain
        }
    }

    private static func words(_ list: String) -> Set<String> {
        Set(list.split(separator: " ").map(String.init))
    }

    private static let slashBlock = (open: "/*", close: "*/")
    private static let quotes = ["\"", "'"]

    private static let plain = LanguageRules()

    private static let swift = LanguageRules(
        keywords: words("""
        actor any as associatedtype async await break case catch class continue default defer deinit do else \
        enum extension fallthrough false fileprivate final for func guard if import in indirect init inout \
        internal is let nil nonisolated open operator override precedencegroup private protocol public repeat \
        rethrows return self Self static struct subscript super switch throw throws true try typealias var \
        where while some
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: ["\""], multiLineStrings: ["\"\"\""],
        typeHeuristic: true, atAttributes: true, hashDirectives: true
    )

    private static let go = LanguageRules(
        keywords: words("""
        break case chan const continue default defer else fallthrough false for func go goto if import \
        interface map nil package range return select struct switch true type var iota
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: ["\"", "'"], multiLineStrings: ["`"], typeHeuristic: true
    )

    private static let jsKeywords = """
    async await break case catch class const continue debugger default delete do else export extends false \
    finally for from function if import in instanceof let new null of return static super switch this throw \
    true try typeof undefined var void while with yield
    """

    private static let javascript = LanguageRules(
        keywords: words(jsKeywords),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, multiLineStrings: ["`"], typeHeuristic: true
    )

    private static let typescript = LanguageRules(
        keywords: words(jsKeywords + """
         abstract as declare enum implements interface keyof namespace never private protected public readonly \
        satisfies type unknown any boolean number string symbol object override
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, multiLineStrings: ["`"], typeHeuristic: true, atAttributes: true
    )

    private static let python = LanguageRules(
        keywords: words("""
        and as assert async await break class continue def del elif else except False finally for from global \
        if import in is lambda None nonlocal not or pass raise return True try while with yield match case
        """),
        lineComment: ["#"], stringDelimiters: quotes, multiLineStrings: ["\"\"\"", "'''"],
        typeHeuristic: true, atAttributes: true
    )

    private static let rust = LanguageRules(
        keywords: words("""
        as async await break const continue crate dyn else enum extern false fn for if impl in let loop match \
        mod move mut pub ref return self Self static struct super trait true type unsafe use where while
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: ["\"", "'"], typeHeuristic: true, lifetimes: true
    )

    private static let ruby = LanguageRules(
        keywords: words("""
        alias and begin break case class def defined do else elsif end ensure false for if in module next nil \
        not or redo rescue retry return self super then true undef unless until when while yield require
        """),
        lineComment: ["#"], stringDelimiters: quotes, typeHeuristic: true
    )

    private static let shell = LanguageRules(
        keywords: words("""
        if then else elif fi for while until do done case esac in function select time export local readonly \
        return exit break continue set unset source alias echo cd
        """),
        lineComment: ["#"], stringDelimiters: quotes
    )

    private static let json = LanguageRules(
        keywords: words("true false null"), stringDelimiters: ["\""], jsonKeys: true
    )

    private static let yaml = LanguageRules(
        keywords: words("true false null yes no on off"),
        lineComment: ["#"], stringDelimiters: quotes
    )

    private static let toml = LanguageRules(
        keywords: words("true false"), lineComment: ["#"], stringDelimiters: quotes,
        multiLineStrings: ["\"\"\"", "'''"]
    )

    private static let markdown = LanguageRules(markdown: true)

    private static let html = LanguageRules(
        blockComment: (open: "<!--", close: "-->"), stringDelimiters: quotes, markup: true
    )

    private static let css = LanguageRules(
        keywords: words("important inherit initial none auto"),
        blockComment: slashBlock, stringDelimiters: quotes, atAttributes: true
    )

    private static let cKeywords = """
    auto break case const continue default do else enum extern for goto if inline register restrict return \
    sizeof static struct switch typedef union volatile while NULL true false
    """

    private static let c = LanguageRules(
        keywords: words(cKeywords), lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, hashDirectives: true
    )

    private static let cpp = LanguageRules(
        keywords: words(cKeywords + """
         alignas alignof bool catch char class constexpr delete explicit export friend mutable namespace new \
        noexcept nullptr operator override private protected public template this throw try typename using \
        virtual int long short unsigned signed void float double final
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, typeHeuristic: true, hashDirectives: true
    )

    private static let objc = LanguageRules(
        keywords: words(cKeywords + " self super nil YES NO id BOOL SEL void int long short float double char"),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, typeHeuristic: true, atAttributes: true, hashDirectives: true
    )

    private static let java = LanguageRules(
        keywords: words("""
        abstract assert boolean break byte case catch char class const continue default do double else enum \
        extends false final finally float for goto if implements import instanceof int interface long native \
        new null package private protected public return short static strictfp super switch synchronized \
        this throw throws transient true try var void volatile while record
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, multiLineStrings: ["\"\"\""], typeHeuristic: true, atAttributes: true
    )

    private static let kotlin = LanguageRules(
        keywords: words("""
        as break by catch class companion constructor continue data do else enum false finally for fun if \
        import in init inline interface internal is lateinit null object open override package private \
        protected public return sealed super suspend this throw true try typealias val var when where while
        """),
        lineComment: ["//"], blockComment: slashBlock,
        stringDelimiters: quotes, multiLineStrings: ["\"\"\""], typeHeuristic: true, atAttributes: true
    )

    private static let sql = LanguageRules(
        keywords: words("""
        select from where and or not null insert into values update set delete create table alter drop index \
        join inner outer left right on as group by order having limit offset union all distinct case when \
        then else end primary key foreign references default unique in is like between exists begin commit \
        rollback with returning
        """),
        lineComment: ["--"], blockComment: slashBlock,
        stringDelimiters: ["'", "\""], caseInsensitive: true
    )
}
