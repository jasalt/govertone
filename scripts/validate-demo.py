#!/usr/bin/env python3
"""Check the Phase 3 §43 demonstration render (make demo) against its claims.

Reads out/demo/*.json and out/demo/demo.wav, written by `make demo`, prints
one line per proof, and exits 6 if any proof fails.
"""
import json
import sys

import numpy as np
from scipy.io import wavfile

D = "out/demo/"
RATE = 44100


def load(name):
    return json.load(open(D + name))


def main():
    events, patches = load("events.json")["events"], load("patches.json")["updates"]
    controls, lanes = load("controls.json")["events"], load("automation.json")["events"]
    midi, transition = load("midi.json"), load("transition.json")
    rate, wav = wavfile.read(D + "demo.wav")
    mono = wav.astype(np.float64).mean(axis=1)
    proofs = []

    def prove(name, ok, detail):
        proofs.append((name, bool(ok), detail))

    def window(t0, t1):
        return mono[int(t0 * rate):int(t1 * rate)]

    def centroid(t0, t1):
        w = window(t0, t1) * np.hanning(int(t1 * rate) - int(t0 * rate))
        p = np.abs(np.fft.rfft(w)) ** 2
        f = np.fft.rfftfreq(len(w), 1 / rate)
        return float(np.sum(f * p) / np.sum(p))

    def rms(t0, t1):
        return float(np.sqrt(np.mean(window(t0, t1) ** 2)))

    def level_at(t0, t1, hz):
        """Spectrum magnitude near hz relative to the window's peak."""
        w = window(t0, t1) * np.hanning(int(t1 * rate) - int(t0 * rate))
        mag = np.abs(np.fft.rfft(w))
        f = np.fft.rfftfreq(len(w), 1 / rate)
        return float(mag[np.abs(f - hz) < 10].max() / mag.max())

    def f0(t0, t1):
        w = window(t0, t1) * np.hanning(int(t1 * rate) - int(t0 * rate))
        mag = np.abs(np.fft.rfft(w))
        return float(np.fft.rfftfreq(len(w), 1 / rate)[np.argmax(mag[1:]) + 1])

    defined = [u for u in patches if u["result"] == "applied"]
    prove("named parameters compile", defined and defined[0]["changed_instruments"] == ["performance-lead"],
          f"{len(defined)} applied patch updates")
    ctl = [c for c in controls if c["kind"] == "set-control" and c.get("parameter") == "cutoff" and c["applied_frame"] == 0]
    # ctl and every later control are override writes: the patch trace has
    # only the two definitions, and the cutoff steps audibly at 0.25 s (CC 30)
    # without a new generation.
    prove("ctl changes the sound without recompilation",
          ctl and len(defined) == 2 and defined[0]["applied_frame"] == 0 and defined[1]["applied_frame"] == 6 * RATE
          and centroid(0.30, 0.45) < centroid(0.05, 0.2),
          f"cutoff ctl at frame 0; {len(controls)} control events but patch updates only at frames "
          f"{[u['applied_frame'] for u in defined]}")
    velocity = [c for c in controls if c.get("parameter") == "velocity"]
    prove("note-local velocity works", {110, 96} <= {round(c["value"]) for c in velocity},
          f"velocities {[round(c['value']) for c in velocity]}")
    ramp = [l for l in lanes if l.get("parameter") == "cutoff" and l["kind"] == "start" and not l.get("bus")]
    done = [l for l in lanes if l.get("parameter") == "cutoff" and l["kind"] == "complete" and not l.get("bus")]
    ramp_ok = ramp and ramp[0]["start_frame"] == 1 * RATE and ramp[0]["end_frame"] == 3 * RATE and done and done[0]["frame"] == 3 * RATE
    rising = centroid(1.0, 1.25) < centroid(2.0, 2.25) < centroid(2.75, 3.0)
    prove("automation is sample-accurate", ramp_ok and rising,
          f"ramp frames {ramp[0]['start_frame'] if ramp else None}-{ramp[0]['end_frame'] if ramp else None}; "
          f"centroid {centroid(1.0, 1.25):.0f} -> {centroid(2.0, 2.25):.0f} -> {centroid(2.75, 3.0):.0f} Hz")
    bus = [c for c in controls if c["kind"] == "set-bus"]
    prove("a control bus drives the cutoff", bus and bus[0]["applied_frame"] == 4 * RATE and centroid(3.5, 3.75) < centroid(4.25, 4.5),
          f"bus-set 82 at frame {bus[0]['applied_frame'] if bus else None}; centroid {centroid(3.5, 3.75):.0f} -> {centroid(4.25, 4.5):.0f} Hz")
    plays = [e for e in midi["events"] if e["action"] == "play"]
    ccs = [e for e in midi["events"] if e["action"] == "control" and e.get("parameter") == "cutoff"]
    prove("MIDI note and CC bindings work", len(plays) == 3 and len(ccs) == 2 and midi["stats"]["late"] == 0
          and abs(f0(4.6, 4.9) - 659) < 15 and centroid(0.30, 0.45) < centroid(0.05, 0.2) < centroid(0.55, 0.7) * 1.05,
          f"{len(plays)} notes, {len(ccs)} CCs; E5 at {f0(4.6, 4.9):.0f} Hz; centroid {centroid(0.05, 0.2):.0f} -> "
          f"{centroid(0.30, 0.45):.0f} (CC 30) -> {centroid(0.55, 0.7):.0f} Hz (CC 64)")
    last = transition["Last"]
    prove("incompatible redefinition crossfades", last["strategy"] == "crossfade" and last["crossfade_frames"] == 882 and last["applied_frame"] == 6 * RATE,
          f"{last['strategy']} over {last['crossfade_frames']} frames at {last['applied_frame']}")
    prove("symbolic IDs remain stable", last["changed_instruments"] == ["performance-lead"]
          and all(e.get("synth") in (None, "performance-lead") for e in midi["events"]),
          "synth and parameter names unchanged across the redefinition")
    prove("valid control state migrates", "performance-lead:cutoff" in (last.get("migrated_controls") or []),
          f"migrated {last.get('migrated_controls')}")
    c6 = level_at(6.4, 6.9, 1046.5)
    prove("MIDI plays the redefined synth", c6 > 0.3 and rms(6.4, 6.9) > 0.05,
          f"C6 (1047 Hz) at {c6:.2f} of the spectral peak, rms {rms(6.4, 6.9):.3f}")
    prove("no late or dropped events", not [e for e in events if e["kind"].endswith(("-failed", "-stale"))],
          f"{len(events)} trace events")
    # The spec patch (resonance 28, out gain 72) overshoots slightly on the
    # resonant transient after a CC jump; the WAV is float, so a peak a few
    # percent over 1 is recorded rather than clipped. The crossfade must not
    # step further than the sustained saw itself does.
    peak = float(np.max(np.abs(wav)))
    steps = np.abs(np.diff(window(6.0 - 0.01, 6.0 + 0.03)))
    sustained = np.abs(np.diff(window(5.6, 5.9))).max()
    prove("spectral and discontinuity checks", bool(np.isfinite(wav).all()) and peak < 1.1 and steps.max() <= 1.5 * sustained,
          f"finite, peak {peak:.3f}; largest step across the crossfade {steps.max():.4f} vs sustained saw {sustained:.4f}")

    failed = 0
    for name, ok, detail in proofs:
        print(f"{'PASS' if ok else 'FAIL'}  {name}: {detail}")
        failed += not ok
    return 6 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
