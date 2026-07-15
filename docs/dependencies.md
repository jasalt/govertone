# Dependency pins

Verified on Fedora Linux 44 x86-64 on 2026-07-13:

```text
go version go1.26.5 linux/amd64
let-go v1.11.1, commit 79b96e56ceca2961009f93d8255fde65275a2efc
Sointu v0.6.0, commit c4d0683be728f4e788528c96b4270ef24f77aff5
```

The versions are pinned in `go.mod` and checksums are in `go.sum`. `lgs version` embeds and prints both upstream commit hashes. Sointu v0.6.0 selects `github.com/ebitengine/oto/v3` for real-time output.

Phase 3 uses a minimal vendored Sointu v0.6.0 subset through the `go.mod` replacement at `third_party/sointu`. The patch adds persistent instrument/per-voice control operands to the pure Go VM because upstream exposes only `Synth.Update`. The original MIT license and an exact change/removal plan are in `third_party/sointu/LICENSE` and `third_party/patches/sointu-controlled-go-vm.md`. Native and WebAssembly control export remain unsupported.

The Sointu public trigger convention is tracker-oriented: note 81 produces concert A4. `lgs` deliberately exposes standard MIDI (`A4 = 69`) and adds 12 only at the Sointu engine boundary. The event trace continues to report MIDI numbers.

The Fedora package names in `scripts/bootstrap-fedora.sh` were checked using `dnf info`: `git`, `golang`, `make`, `gcc`, `pkgconf-pkg-config`, `alsa-lib-devel`, `pipewire`, `pipewire-alsa`, `pipewire-pulseaudio`, `python3`, `python3-numpy`, and `python3-scipy`.
