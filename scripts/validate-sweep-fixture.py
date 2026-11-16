#!/usr/bin/env python3
"""Validate the Phase 3 §27.3 filter-sweep and §27.4 pan-sweep renders.

Both fixtures hold one note and ramp a parameter between 0.5 s and 3.5 s.
The audio is cut into 250 ms windows across the ramp.

filter: the spectral centroid rises (allowing small dips), ends at least
        twice as high as it starts, and the fundamental does not move.
pan:    the left/right energy balance moves monotonically from left to
        right, no sample clips, and total power stays within 3 dB.
"""
import argparse
import json
import sys

import numpy as np
from scipy.io import wavfile

RAMP = (0.5, 3.5)
WINDOW = 0.25


def windows(samples, rate):
    start, end = (int(t * rate) for t in RAMP)
    step = int(WINDOW * rate)
    return [samples[i:i + step] for i in range(start, end - step + 1, step)]


def filter_sweep(samples, rate):
    failures, centroids, pitches = [], [], []
    for w in windows(samples, rate):
        mono = w.mean(axis=1) * np.hanning(len(w))
        mag = np.abs(np.fft.rfft(mono))
        freqs = np.fft.rfftfreq(len(mono), 1 / rate)
        power = mag * mag
        centroids.append(float(np.sum(freqs * power) / np.sum(power)))
        pitches.append(float(freqs[np.argmax(mag[1:]) + 1]))
    rises = sum(b > a for a, b in zip(centroids, centroids[1:]))
    if rises < 0.8 * (len(centroids) - 1):
        failures.append(f"centroid rises in only {rises} of {len(centroids) - 1} steps")
    if centroids[-1] < 2 * centroids[0]:
        failures.append(f"centroid {centroids[0]:.0f} -> {centroids[-1]:.0f} Hz is not a significant rise")
    if max(pitches) - min(pitches) > 10:
        failures.append(f"dominant pitch moved {min(pitches):.0f} -> {max(pitches):.0f} Hz")
    return failures, {"centroid_hz": centroids, "dominant_hz": pitches}


def pan_sweep(samples, rate):
    failures, balance, power = [], [], []
    for w in windows(samples, rate):
        left, right = np.sqrt(np.mean(w * w, axis=0))
        balance.append(float((right - left) / (right + left)))
        power.append(float(left * left + right * right))
    if any(b < a - 1e-6 for a, b in zip(balance, balance[1:])):
        failures.append("balance does not move monotonically left to right")
    if balance[0] > -0.5 or balance[-1] < 0.5:
        failures.append(f"balance {balance[0]:.2f} -> {balance[-1]:.2f} does not cross from left to right")
    spread = 10 * np.log10(max(power) / min(power))
    if spread > 3:
        failures.append(f"total power varies {spread:.1f} dB across the sweep")
    if np.max(np.abs(samples)) >= 1:
        failures.append("clipping")
    return failures, {"balance": balance, "power_spread_db": float(spread)}


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--kind", choices=["filter", "pan"], required=True)
    p.add_argument("--input", required=True)
    p.add_argument("--report")
    args = p.parse_args()
    rate, samples = wavfile.read(args.input)
    samples = samples.astype(np.float64)
    if not np.isfinite(samples).all():
        print("non-finite samples", file=sys.stderr)
        return 6
    failures, metrics = (filter_sweep if args.kind == "filter" else pan_sweep)(samples, rate)
    report = {"kind": args.kind, "valid": not failures, "failures": failures, **metrics}
    text = json.dumps(report, indent=2) + "\n"
    if args.report:
        open(args.report, "w").write(text)
    for failure in failures:
        print(f"{args.kind} sweep: {failure}", file=sys.stderr)
    return 6 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
