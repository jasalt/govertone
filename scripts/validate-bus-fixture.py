#!/usr/bin/env python3
"""Spectral check of out/fixtures/bus-two-synths.wav (testdata/buses/two-synths.lg).

One bus drives two synths' output level: A3 at bus x 64 and E5 at
48 - bus x 48. The bus is 0 for beat 0-1, 0.5 for beat 1-2, and 1 after
the ramp over beat 2-3. The A3 and E5 peak magnitudes must follow those
gains: zero, half, and full for A3, and the reverse for E5.
"""
import sys

import numpy as np
from scipy.io import wavfile

path = sys.argv[1] if len(sys.argv) > 1 else "out/fixtures/bus-two-synths.wav"
rate, samples = wavfile.read(path)
left = samples[:, 0].astype(np.float64)


def peak(segment, frequency):
    spectrum = np.abs(np.fft.rfft(segment * np.hanning(len(segment))))
    bins = np.fft.rfftfreq(len(segment), 1 / rate)
    return spectrum[np.abs(bins - frequency) < 8].max()


windows = {"bus 0": (2000, 20000), "bus 0.5": (24000, 42000), "bus 1": (68000, 86000)}
a3 = {name: peak(left[a:b], 220.0) for name, (a, b) in windows.items()}
e5 = {name: peak(left[a:b], 659.26) for name, (a, b) in windows.items()}
print("A3", {k: round(float(v), 1) for k, v in a3.items()})
print("E5", {k: round(float(v), 1) for k, v in e5.items()})
failures = []
full_a3, full_e5 = a3["bus 1"], e5["bus 0"]
for name, want_a3, want_e5 in [("bus 0", 0.0, 1.0), ("bus 0.5", 0.5, 0.5), ("bus 1", 1.0, 0.0)]:
    for label, got, want in [("A3", a3[name] / full_a3, want_a3), ("E5", e5[name] / full_e5, want_e5)]:
        if abs(got - want) > 0.02:
            failures.append(f"{label} at {name}: relative level {got:.3f}, want {want}")
if failures:
    print("\n".join(failures))
    sys.exit(1)
print("bus fixture: ok")
