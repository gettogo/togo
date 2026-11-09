#!/usr/bin/env python3
"""How smooth a hero take is: decodes it at 60 fps, takes the mean absolute difference of each
frame from the one before (grey, downscaled to 100x218, 0-255), and per 0.5 s window counts the
frames that changed (diff > 0.05). A window in motion should have close to 30/30; a still window
has 0. Long runs of repeated frames inside motion, or one big diff after them, are dropped frames.

  python3 scripts/hero-smoothness.py <video> [--table]
"""
import subprocess, sys
import numpy as np

src = sys.argv[1]
W, H, FPS = 100, 218, 60
raw = subprocess.run(["ffmpeg", "-v", "error", "-i", src, "-vf", f"fps={FPS},scale={W}:{H},format=gray",
                      "-f", "rawvideo", "-"], capture_output=True, check=True).stdout
f = np.frombuffer(raw, np.uint8).reshape(-1, H, W).astype(np.float32)
d = np.abs(np.diff(f, axis=0)).mean(axis=(1, 2))
moved = d > 0.05
win = FPS // 2
print(f"{src}: {len(f)} frames, {len(f) / FPS:.2f} s")
rows = []
for s in range(0, len(d), win):
    m = moved[s:s + win]
    rows.append((s / FPS, int(m.sum()), len(m), float(d[s:s + win].max())))
if "--table" in sys.argv:
    for t, u, n, mx in rows:
        if u: print(f"  {t:5.1f} s  {u:2d}/{n}  max diff {mx:5.1f}")
# Motion segments: changed frames with gaps of at most 6 repeats between them.
segs, start, last = [], None, None
for i, m in enumerate(moved):
    if not m: continue
    if start is None or i - last > 6:
        if start is not None: segs.append((start, last))
        start = i
    last = i
if start is not None: segs.append((start, last))
worst = []
for a, b in segs:
    n = b - a + 1
    if n < 8: continue
    u = int(moved[a:b + 1].sum())
    worst.append((u / n, a, b, u, n, float(d[a:b + 1].max())))
print("motion segments (>= 8 frames):")
for r, a, b, u, n, mx in worst:
    print(f"  {a / FPS:5.2f}-{(b + 1) / FPS:5.2f} s  {u:3d}/{n:<3d} changed ({r * 100:3.0f}%)  max diff {mx:5.1f}")
# Drops: a repeated frame between real movement (diff > 1 within 3 frames on both sides), and
# jumps: movement that starts with a diff > 15, its first frames missing.
drops = [i for i in range(3, len(d) - 3) if not moved[i] and d[i - 3:i].max() > 1 and d[i + 1:i + 4].max() > 1]
jumps = [i for i in range(3, len(d)) if d[i] > 15 and d[i - 3:i].max() < 0.5]
print(f"repeated frames inside motion: {len(drops)}" + (f" (at {', '.join(f'{i / FPS:.2f}' for i in drops[:20])} s)" if drops else ""))
print(f"jumps from still: {len(jumps)}" + (f" (at {', '.join(f'{i / FPS:.2f} s, diff {d[i]:.1f}' for i in jumps)})" if jumps else ""))
if worst:
    changed, total = sum(w[3] for w in worst), sum(w[4] for w in worst)
    print(f"overall in motion: {changed}/{total} frames changed")
    # One number to pick the best of several takes: the share of motion frames that changed,
    # less a point per repeat inside motion and five per jump.
    print(f"score {100 * changed / total - len(drops) - 5 * len(jumps):.1f}")
