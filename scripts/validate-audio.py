#!/usr/bin/env python3
"""Independent NumPy/SciPy-compatible cross-check for lgs float WAV output."""
import argparse, json, struct
import numpy as np


def read_wav(path):
    data = open(path, "rb").read()
    if data[:4] != b"RIFF" or data[8:12] != b"WAVE":
        raise ValueError("not RIFF/WAVE")
    pos, fmt, payload = 12, None, None
    while pos + 8 <= len(data):
        name, size = data[pos:pos+4], struct.unpack_from("<I", data, pos+4)[0]
        pos += 8
        chunk = data[pos:pos+size]
        if len(chunk) != size:
            raise ValueError("truncated chunk")
        if name == b"fmt ":
            fmt = struct.unpack_from("<HHI", chunk)
            bits = struct.unpack_from("<H", chunk, 14)[0]
        elif name == b"data":
            payload = chunk
        pos += size + size % 2
    if fmt is None or payload is None:
        raise ValueError("missing fmt/data")
    encoding, channels, rate = fmt
    if channels != 2:
        raise ValueError("expected stereo")
    if encoding == 3 and bits == 32:
        samples = np.frombuffer(payload, dtype="<f4").reshape(-1, 2).astype(np.float64)
    elif encoding == 1 and bits == 16:
        samples = np.frombuffer(payload, dtype="<i2").reshape(-1, 2) / 32768.0
    else:
        raise ValueError(f"unsupported encoding {encoding}/{bits}")
    return rate, samples


def metrics(rate, samples):
    n = min(32768, len(samples))
    n = 1 << (n.bit_length() - 1)
    mono = samples[:n].mean(axis=1)
    win = np.hanning(n)
    mag = np.abs(np.fft.rfft(mono * win))
    peak_bin = int(np.argmax(mag[1:]) + 1) if n else 0
    freqs = np.fft.rfftfreq(n, 1 / rate) if n else np.array([0])
    power = mag * mag
    return {
        "sample_rate": rate, "channels": 2, "frames": len(samples),
        "peak": np.max(np.abs(samples), axis=0).tolist(),
        "rms": np.sqrt(np.mean(samples*samples, axis=0)).tolist(),
        "dc": np.mean(samples, axis=0).tolist(),
        "finite": bool(np.isfinite(samples).all()),
        "dominant_frequency_hz": float(freqs[peak_bin]),
        "spectral_centroid_hz": float(np.sum(freqs*power)/np.sum(power)) if np.sum(power) else 0.0,
    }


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--input", required=True)
    p.add_argument("--report")
    args = p.parse_args()
    report = metrics(*read_wav(args.input))
    text = json.dumps(report, indent=2) + "\n"
    if args.report:
        open(args.report, "w").write(text)
    else:
        print(text, end="")
    if not report["finite"]:
        raise SystemExit(6)

if __name__ == "__main__":
    main()
