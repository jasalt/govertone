#!/usr/bin/env python3
"""Check out/fixtures/midi-scale*.wav (testdata/midi/scale.midilog).

The log plays C4, D4, E4, F4 for 0.25 s each. Each quarter second must have
its note as the dominant pitch, and its loudness must follow velocity x level:
D4 (velocity 100) and E4 (velocity 60) at level 64, then F4 (velocity 127) at
level 127 after the CC. The two renders, made with different block sizes, must
be identical sample for sample.
"""
import sys

import numpy as np
from scipy.io import wavfile

path, other = sys.argv[1], sys.argv[2]
rate, samples = wavfile.read(path)
_, samples_other = wavfile.read(other)
failures = []
if samples.shape != samples_other.shape or not np.array_equal(samples, samples_other):
    failures.append("renders with different block sizes differ")
left = samples[:, 0].astype(np.float64)

notes = [("C4", 261.63), ("D4", 293.66), ("E4", 329.63), ("F4", 349.23)]
rms = []
for index, (name, frequency) in enumerate(notes):
    # Skip the first and last 10 ms of each window: note edges.
    lo, hi = int((index * 0.25 + 0.01) * rate), int(((index + 1) * 0.25 - 0.01) * rate)
    segment = left[lo:hi]
    spectrum = np.abs(np.fft.rfft(segment * np.hanning(len(segment))))
    found = np.fft.rfftfreq(len(segment), 1 / rate)[spectrum.argmax()]
    rms.append(float(np.sqrt(np.mean(segment**2))))
    print(f"{name}: dominant {found:.1f} Hz, rms {rms[-1]:.4f}")
    if abs(found - frequency) > 6:
        failures.append(f"{name}: dominant pitch {found:.1f} Hz, want {frequency}")

# D4 and E4 differ only in velocity: 100 vs 60 at the same level.
ratio = rms[2] / rms[1]
if abs(ratio - 60 / 100) > 0.03:
    failures.append(f"E4/D4 loudness {ratio:.3f}, want {60 / 100} from the velocities")
# F4 comes after CC 7 raised the level from 64 to 127, at velocity 127.
ratio = rms[3] / rms[1]
if abs(ratio - (127 / 127) * (127 / 64) / (100 / 127)) > 0.1:
    failures.append(f"F4/D4 loudness {ratio:.3f} does not match velocity and CC level")
# After the last note-off (1.0 s) the output is silent.
tail = left[int(1.05 * rate) :]
if np.abs(tail).max() > 1e-4:
    failures.append(f"output continues after the final note-off: peak {np.abs(tail).max():.5f}")
if failures:
    print("\n".join(failures))
    sys.exit(1)
print("midi fixture: ok")
