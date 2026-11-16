# Expanded song demos

The originals are unchanged. Run these programs from the repository root:
`load-file` paths in the v2 arrangements are relative to that directory.
They schedule absolute beats from frame zero and are intended for offline
rendering, not loading into an already-running REPL timeline.

| Song | Audio | Length incl. tail | Sounds | Reserved voices | Peak simultaneous notes | VMs | Float peak |
|---|---|---:|---:|---:|---:|---:|---:|
| [Hectic electronic beat v2](hectic-electronic-beat-v2.lg) | [MP3](audio/hectic-electronic-beat-v2.mp3) | 30 s | 16 | 111 | 39 | 4 | 0.8303 |
| [Game of Thrones theme v2](game-of-thrones-theme-v2.lg) | [MP3](audio/game-of-thrones-theme-v2.mp3) | 40 s | 12 | 96 | 39 | 3 | 0.3880 |
| [Ode to Joy orchestra](ode-to-joy-orchestra.lg) | [MP3](audio/ode-to-joy-orchestra.mp3) | 36 s | 64 | 256 | 256 | 8 | 0.6068 |

These are measured scheduler-active notes, not just reserved voice capacity;
release tails can continue after a note is released. All instruments in each
patch are actually used. The float renders have no NaN/Inf, no full-scale
overs, no late/dropped events and no voice stealing. These checks do not
replace listening tests or establish real-time performance on other machines.

## Arrangements

**Hectic electronic beat v2** reuses the original six-instrument, 18-bar groove
and automation. Ten new sounds add four panned sustained pads, four interlocking
arps, sub bass and descending metallic tom fills. Pads enter in bar three,
arps in bar five, and the drop adds sustained arp chords from bar nine.
The mix is deliberately quieter per added part: shards sum at unity gain.

**Game of Thrones theme v2** reuses the existing short melody and timing
(Ramin Djawadi's composition; synthetic fan arrangement, not the original
score or recording). It replaces the three built-in timbres and adds divided
strings, horns, flute, reed, harp and timpani. Upper melody doubles enter in
the second half over sustained four-note section voicings. This is still the
compact theme sketch, not a full transcription.

**Ode to Joy orchestra** uses Beethoven's public-domain melody, with new
block-chord orchestration. It reaches both current default compiler limits:
64 instruments and 256 voices. There are 16 section timbres, each represented
by four independently detuned and panned desks with four voices. Thus **64
sounds does not mean 64 different instrument families**: the desks emulate
players within a section. Strings enter first, then winds and brass; the
second statement uses the entire ensemble. The closing tonic at beat 64
plays all 256 voice slots together.

Sections: first/second violins, viola, cello, double bass, flute, piccolo,
oboe, clarinet, bassoon, horn, trumpet, trombone, tuba, harp and celesta.
These are inexpensive oscillator/filter/envelope approximations, not samples
or physical models. A reduced output gain leaves headroom without changing
the engine's unity-gain summation. Eight packed VMs suffice; the separate
16-shard ceiling is not the limiting factor here. This demonstrates the
maximum *configured default* sound/voice counts, not a universal CPU limit.

## Reproduce and validate

```sh
CGO_ENABLED=0 go build -o out/lgs ./cmd/lgs
nix develop -c python3 scripts/validate-multisound-songs.py
```

The script renders all three songs to `out/multisound-songs/`, checks stereo
length, finite samples, peak/RMS, exact event timing, matched releases,
absence of voice stealing, used instrument/shard counts and simultaneous
polyphony. It writes `measurements.json` alongside the WAVs and event traces.
Individual render commands are also at the top of each song.

The checked-in previews were encoded without normalization or limiting:

```sh
for song in hectic-electronic-beat-v2 game-of-thrones-theme-v2 ode-to-joy-orchestra; do
  ffmpeg -y -i out/multisound-songs/$song.wav \
    -codec:a libmp3lame -q:a 4 examples/audio/$song.mp3
done
```
