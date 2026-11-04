import Foundation

/// Themes that ship with the app.
///
/// Beacon backgrounds and foregrounds are the brand tokens (`ink` and `snow`, swapped for
/// light). Every coloured ANSI entry (1-6, 9-14) clears 4.5:1 on its background so agent
/// diffs and status colours stay legible; 0, 7, 8 and 15 are exempt because they exist to
/// blend into or stand out from the background. No entry uses the accent, which is reserved
/// for "needs you".
public enum BuiltInThemes {
    public static let all: [TerminalTheme] = [beaconDark, beaconLight, plain]

    static let beaconDark = TerminalTheme(
        id: "beacon-dark",
        name: "Beacon Dark",
        ansi: [
            "#1C2028", "#F07A72", "#8CC97A", "#E3C06B", "#7AA7E8", "#C99AE6", "#6CC7C0", "#C4C8CF",
            "#5F6774", "#FF9A90", "#A8DE96", "#F2D488", "#9DBFF2", "#DDB5F2", "#8EDDD6", "#F5F4F1",
        ],
        foreground: "#ECEAE6",
        background: "#101318",
        cursor: "#ECEAE6",
        selection: "#252A33"
    )

    static let beaconLight = TerminalTheme(
        id: "beacon-light",
        name: "Beacon Light",
        ansi: [
            "#101318", "#B3261E", "#2E6B1F", "#7A5A00", "#1F5FAD", "#8A3FA0", "#0F6B66", "#8B93A1",
            "#5F6774", "#9E1F18", "#245A17", "#664A00", "#194F91", "#74328A", "#0B5955", "#FFFFFF",
        ],
        foreground: "#101318",
        background: "#ECEAE6",
        cursor: "#101318",
        selection: "#CFCBC4"
    )

    /// The xterm defaults, for people who want their terminal to look like every other one.
    static let plain = TerminalTheme(
        id: "plain",
        name: "Plain",
        ansi: [
            "#000000", "#CD0000", "#00CD00", "#CDCD00", "#0000EE", "#CD00CD", "#00CDCD", "#E5E5E5",
            "#7F7F7F", "#FF0000", "#00FF00", "#FFFF00", "#5C5CFF", "#FF00FF", "#00FFFF", "#FFFFFF",
        ],
        foreground: "#FFFFFF",
        background: "#000000",
        cursor: "#FFFFFF",
        selection: "#4D4D4D"
    )
}
