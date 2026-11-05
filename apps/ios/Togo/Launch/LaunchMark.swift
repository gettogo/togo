import SwiftUI

/// The Togo mark drawn from `brand/togo-mark-light.svg` and `-dark.svg`: a prompt chevron, four
/// paw prints walking up and to the right, and the beacon they walk towards. The two files
/// differ only in colour, which the palette's light and dark values already carry.
///
/// Every part takes its own progress so the launch can stroke, walk and light it in turn.
struct LaunchMark: View {
    var chevron: Double = 1
    var paws: [Double] = Array(repeating: 1, count: MarkGeometry.paws.count)
    var beaconScale: Double = 1
    var beaconOpacity: Double = 1
    /// Width of the drawn mark, from the chevron's left cap to the beacon's right edge. The
    /// frame is the SVG's whole box scaled to match, so the mark keeps the file's optical centre.
    var width: CGFloat = 132

    var body: some View {
        let unit = width / MarkGeometry.inkWidth
        let size = MarkGeometry.viewBox * unit
        ZStack {
            MarkPart(path: MarkGeometry.chevron)
                .trim(from: 0, to: chevron)
                .stroke(
                    Color.togoSnow,
                    style: StrokeStyle(lineWidth: MarkGeometry.chevronWidth * unit, lineCap: .round, lineJoin: .round)
                )
            ForEach(MarkGeometry.paws.indices, id: \.self) { index in
                MarkPart(path: MarkGeometry.paw(index))
                    .fill(Color.togoSnow)
                    .opacity(paws[index])
            }
            MarkPart(path: MarkGeometry.beacon)
                .fill(Color.togoAccent)
                .scaleEffect(beaconScale, anchor: MarkGeometry.beaconAnchor)
                .opacity(beaconOpacity)
        }
        .frame(width: size, height: size)
    }
}

/// One part of the mark in the SVG's 64-unit box, scaled to whatever frame it is given.
private struct MarkPart: Shape {
    let path: Path

    func path(in rect: CGRect) -> Path {
        let unit = min(rect.width, rect.height) / MarkGeometry.viewBox
        return path.applying(CGAffineTransform(scaleX: unit, y: unit)).offsetBy(dx: rect.minX, dy: rect.minY)
    }
}

/// Coordinates copied from the SVG, including the group's `translate(-.2 .4)` nudge that
/// centres the mark optically.
private enum MarkGeometry {
    static let viewBox: CGFloat = 64
    static let chevronWidth: CGFloat = 4.5
    /// The drawn extent in box units: the stroked chevron's round cap to the beacon's edge.
    static let inkWidth = beacon.boundingRect.maxX
        - chevron.strokedPath(StrokeStyle(lineWidth: chevronWidth, lineCap: .round)).boundingRect.minX
    static let nudge = CGAffineTransform(translationX: -0.2, y: 0.4)

    static let chevron = Path { path in
        path.move(to: CGPoint(x: 9, y: 23))
        path.addLine(to: CGPoint(x: 19.5, y: 31))
        path.addLine(to: CGPoint(x: 9, y: 39))
    }
    .applying(nudge)

    /// Each print's `translate(x y) rotate(deg) scale(.9)`, left to right.
    static let paws: [(x: CGFloat, y: CGFloat, degrees: CGFloat)] = [
        (26.16, 42.57, -26.6), (31.28, 37.54, -36.6), (38.08, 35.23, -26.6), (43.21, 30.2, -36.6),
    ]

    /// The `#pw` symbol: a pad with three toes ahead of it, pointing along +x.
    private static let print = Path { path in
        path.addEllipse(in: CGRect(x: -1.7, y: -1.95, width: 3.4, height: 3.9))
        for toe in [CGPoint(x: 2.55, y: -1.7), CGPoint(x: 3.1, y: 0), CGPoint(x: 2.55, y: 1.7)] {
            path.addEllipse(in: CGRect(x: toe.x - 0.85, y: toe.y - 0.85, width: 1.7, height: 1.7))
        }
    }

    static func paw(_ index: Int) -> Path {
        let placement = paws[index]
        let transform = CGAffineTransform(scaleX: 0.9, y: 0.9)
            .concatenating(CGAffineTransform(rotationAngle: placement.degrees * .pi / 180))
            .concatenating(CGAffineTransform(translationX: placement.x, y: placement.y))
            .concatenating(nudge)
        return print.applying(transform)
    }

    private static let beaconCentre = CGPoint(x: 54, y: 24.5).applying(nudge)
    private static let beaconRadius: CGFloat = 4.6

    static let beacon = Path(ellipseIn: CGRect(
        x: beaconCentre.x - beaconRadius, y: beaconCentre.y - beaconRadius,
        width: 2 * beaconRadius, height: 2 * beaconRadius
    ))

    /// The beacon grows from its own centre, not the mark's.
    static let beaconAnchor = UnitPoint(x: beaconCentre.x / viewBox, y: beaconCentre.y / viewBox)
}
