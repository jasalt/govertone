#!/usr/bin/env python3
"""Click metric for patch transitions (testdata/transitions).

Both fixtures play a sustained sine and redefine the synth structurally at
frame 22050 (0.5 s). The crossfade render must be identical to the hard-swap
render before the update, finite everywhere, free of clipping, silent after
the 882-frame fade, and its largest sample-to-sample step around the update
must stay at the level of the sustained tone, while the hard swap must click.
"""
import sys

import numpy as np
from scipy.io import wavfile

UPDATE, FADE = 22050, 882
crossfade_path, hard_path = sys.argv[1:3]
_, crossfade = wavfile.read(crossfade_path)
_, hard = wavfile.read(hard_path)
crossfade, hard = crossfade.astype(np.float64), hard.astype(np.float64)


def max_step(x):
    return np.abs(np.diff(x[:, 0])).max()


sustained = max_step(crossfade[UPDATE - 2000:UPDATE])
window = slice(UPDATE - 200, UPDATE + FADE + 200)
fade_step, hard_step = max_step(crossfade[window]), max_step(hard[window])
print(f"max step: sustained {sustained:.4f}, crossfade {fade_step:.4f}, hard swap {hard_step:.4f}")
failures = []
if not np.isfinite(crossfade).all():
    failures.append("non-finite samples")
if np.abs(crossfade).max() >= 1.0:
    failures.append("clipping")
if not np.array_equal(crossfade[:UPDATE], hard[:UPDATE]):
    failures.append("renders differ before the update")
if np.abs(crossfade[UPDATE + FADE:]).max() != 0:
    failures.append("old engine audible after the fade")
if fade_step > 1.5 * sustained:
    failures.append(f"crossfade clicks: step {fade_step:.4f} > 1.5 x sustained {sustained:.4f}")
if hard_step < 5 * sustained:
    failures.append(f"hard-swap baseline does not click ({hard_step:.4f}); the metric is not calibrated")
if failures:
    print("\n".join(failures))
    sys.exit(1)
print("transition fixtures: ok")
