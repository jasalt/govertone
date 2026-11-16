#!/usr/bin/env python3
"""Render the expanded demos and check real, not merely reserved, polyphony.

Run from the repository root; needs numpy and scipy (nix develop).
Usage: python3 scripts/validate-multisound-songs.py [--lgs out/lgs]
Audio and traces are written under out/multisound-songs/.
"""
import argparse
import json
from pathlib import Path
import subprocess

import numpy as np
from scipy.io import wavfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lgs', default='out/lgs')
    args = parser.parse_args()
    output = Path('out/multisound-songs')
    output.mkdir(parents=True, exist_ok=True)
    songs = [
        ('hectic-electronic-beat-v2', 28, 16, 39, 4),
        ('game-of-thrones-theme-v2', 38, 12, 39, 3),
        ('ode-to-joy-orchestra', 34, 64, 256, 8),
    ]
    results = []
    for name, duration, sounds, polyphony, shards in songs:
        wav = output / f'{name}.wav'
        trace = output / f'{name}.events.json'
        subprocess.run([
            args.lgs, 'render', '--input', f'examples/{name}.lg',
            '--output', str(wav), '--duration', f'{duration}s', '--tail', '2s',
            '--event-trace', str(trace),
        ], check=True)
        rate, samples = wavfile.read(wav)
        assert samples.dtype.kind == 'f', 'expected float WAV (no clipping hidden by PCM conversion)'
        assert rate == 44100 and samples.shape == ((duration + 2) * rate, 2)
        assert np.isfinite(samples).all(), name
        peak = float(np.max(np.abs(samples)))
        rms = float(np.sqrt(np.mean(samples.astype(np.float64) ** 2)))
        assert 0.001 < rms and peak < 0.98, (name, peak, rms)
        active = set()
        high_water = 0
        used_sounds, used_shards = set(), set()
        for event in json.loads(trace.read_text())['events']:
            assert event['scheduled_frame'] == event['applied_frame'], event
            if event['kind'] == 'trigger':
                assert event['voice'] not in active, ('voice stolen', event)
                active.add(event['voice'])
                used_sounds.add(event['instrument'])
                used_shards.add(event['shard'])
            elif event['kind'] == 'release':
                assert event['voice'] in active, ('unmatched release', event)
                active.remove(event['voice'])
            high_water = max(high_water, len(active))
        assert not active, ('unfinished notes', name)
        assert (len(used_sounds), high_water, len(used_shards)) == (sounds, polyphony, shards)
        result = dict(song=name, sounds=sounds, active_voices=high_water,
                      shards=shards, peak=peak, rms=rms)
        results.append(result)
        print(json.dumps(result), flush=True)
    (output / 'measurements.json').write_text(json.dumps(results, indent=2) + '\n')


if __name__ == '__main__':
    main()
