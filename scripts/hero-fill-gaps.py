#!/usr/bin/env python3
"""Fills the frames the simulator dropped mid-motion, and only those.

The Simulator renders about 40 fps while something moves, so a 60 fps take repeats a frame here
and there inside an animation. This finds each short run (1 to MAX frames) of a repeated frame
with real movement on both sides (the same measure as hero-smoothness.py: mean absolute grey
difference on 100x218, repeat < 0.3, movement > 1), drops those repeats, and lets ffmpeg's
minterpolate rebuild them from the real frames either side: by default a blend of the two
(--mode blend), which reads as motion blur and draws nothing new. Motion-compensated
interpolation (--mode mci) tears text that scrolls more than a few pixels a frame, so it is
not the default. Still stretches,
cuts and longer freezes are left alone: nothing is made up across them.

  python3 scripts/hero-fill-gaps.py <raw take> <out.mkv> [--max 3] [--mode blend|mci]

The output is 804x1748, 60 fps, lossless; the website's encoder takes it from there.
"""
import subprocess, sys, tempfile, os
import numpy as np

src, out = sys.argv[1], sys.argv[2]
MAX = int(sys.argv[sys.argv.index("--max") + 1]) if "--max" in sys.argv else 3
MODE = sys.argv[sys.argv.index("--mode") + 1] if "--mode" in sys.argv else "blend"
W, H, FPS = 100, 218, 60
raw = subprocess.run(["ffmpeg", "-v", "error", "-i", src, "-vf", f"scale=804:1748:flags=lanczos,fps={FPS},scale={W}:{H},format=gray",
                      "-f", "rawvideo", "-"], capture_output=True, check=True).stdout
f = np.frombuffer(raw, np.uint8).reshape(-1, H, W).astype(np.float32)
d = np.abs(np.diff(f, axis=0)).mean(axis=(1, 2))  # d[k]: frame k to k+1
REPEAT, MOVE = 0.3, 1.0
drop, j = [], 1
while j < len(f):
    if d[j - 1] < REPEAT:
        r = 0
        while j + r < len(f) and d[j + r - 1] < REPEAT:
            r += 1
        end = j + r  # first frame that differs
        if r <= MAX and j >= 2 and end < len(f) and d[j - 2] > MOVE and d[end - 1] > MOVE:
            drop.extend(range(j, end))
        j = end
    else:
        j += 1
print(f"hero-fill-gaps: {len(drop)} repeated frames inside motion rebuilt"
      + (f" (at {', '.join(f'{i / FPS:.2f}' for i in drop)} s)" if drop else ""))
keep = "1" if not drop else "not(" + "+".join(f"eq(n,{i})" for i in drop) + ")"
with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False) as t:
    t.write(f"scale=804:1748:flags=lanczos,fps={FPS},select='{keep}',"
            f"minterpolate=fps={FPS}:" + ("mi_mode=mci:mc_mode=aobmc:me_mode=bidir:vsbmc=1" if MODE == "mci" else "mi_mode=blend"))
try:
    subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", src, "-an", "-filter_script:v", t.name,
                    "-c:v", "libx264rgb", "-qp", "0", "-preset", "veryfast", out], check=True)
finally:
    os.unlink(t.name)
