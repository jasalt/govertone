# Audio validation

`lgs analyze` is the primary Go analyzer. It parses RIFF chunks defensively, accepts IEEE float32 and PCM16 stereo, rejects malformed lengths/formats, and reports frame count, finite status, channel peak/RMS/DC, clipping, active percentage, stereo correlation, zero runs, maximum first difference, frequency, centroid, and a quantized SHA-256 fingerprint.

## Spectrum and pitch

The analyzer selects the highest-energy power-of-two window up to 32,768 frames, applies a Hann window, and runs an in-place radix-2 FFT. The dominant non-DC bin is refined by parabolic interpolation of adjacent log magnitudes. Spectral centroid uses squared magnitude. A positive-going zero-crossing estimate supplies an independent time-domain frequency. For steady A4, both must be within 1 Hz and should be within 0.25 Hz.

The sine fixture thresholds in `testdata/expectations/single-note.json` require non-silence, peak below 0.9, no clipping, low DC, balanced identical channels, and 439-441 Hz. Harmonic purity is calibrated to -25 dB. Lead validation expects a higher centroid and at least three harmonics rather than exact bins.

## Timing and onset

The event trace is primary timing truth: every `scheduled_frame` must equal `applied_frame`. At 120 BPM, eighth-note events are 11,025 frames apart. Waveform onset uses a short-time RMS threshold and allows the patch attack tolerance in `timing.json`; attack latency is not confused with scheduler jitter.

## Block comparison and discontinuities

The integration suite renders at 64, 128, 256, 512, and 1024 frames. It requires equal traces/frame counts, maximum sample difference at most `1e-6`, and RMS difference at most `1e-8`. Near-zero runs and maximum sample differences expose sustained dropouts and clicks, especially at callback boundaries.

## Golden policy

Metric ranges are compatibility truth. The report also hashes samples after rounding to `1e-6`; this stronger fingerprint is useful on the same architecture but is not by itself a cross-platform failure because upstream floating-point behavior can change harmless low bits.

## Independent cross-check

```sh
python3 scripts/validate-audio.py --input out/fixtures/single-note.wav
```

The NumPy analyzer independently parses float WAV and computes frame count, peak, RMS, DC, finite status, an FFT-bin pitch, and centroid. Go/Python frame counts must match exactly; level metrics should agree within `1e-6`; un-interpolated Python pitch may differ by one FFT-bin (about 1.35 Hz at the standard window), and centroid tolerance is 2 Hz.
