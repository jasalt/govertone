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

`flake.nix` defines a Nix dev shell with the same tools: Go 1.26 from `nixpkgs#go_1_26`, gcc, pkg-config, alsa-lib, GNU make, and Python with numpy and scipy. `flake.lock` pins nixpkgs. `make acceptance` passes in the shell with `CGO_ENABLED=1`, including `test-race` and the Python analysis. Update the pin with `nix flake update`.

## Upstream let-go upgrade

> **Pending upstream dependency (#27):** `lgs` stays on let-go v1.11.1 until **let-go v1.14** is tagged. That release adds the `*audio*` seam the planned `letgo` output sink needs ([§4a](multi-instance-engine.md#4a-mixing-and-output-sinks)). Upgrade once, straight to v1.14+, rather than to v1.13.

The upgrade has to change these together:

- **Go 1.27.** let-go v1.13.0+ declares `go 1.27`. Bump the `go` directive in `go.mod`, `go-version` in `.github/workflows/ci.yml`, `go = pkgs.go_1_26` → `pkgs.go_1_27` in `flake.nix`, and the version notes here and in the README. Fedora's `golang` package defaults to `GOTOOLCHAIN=local`, so `scripts/bootstrap-fedora.sh` works only once Fedora ships Go 1.27. Until then, use the Nix shell.
- **Incomplete-input detection.** Replace `incomplete()` in `internal/lisp/runtime.go`, which matches error text, with `api.IsIncomplete` (available since v1.13.0).
- **Sink.** Register the `letgo` sink as described in #27.

let-go v1.13.0 was checked with Go 1.27.3 from Nix: it builds with cgo, `go test -race ./internal/...` passes, and its new graphics dependencies are not linked into `lgs`.

The Fedora package names in `scripts/bootstrap-fedora.sh` were checked using `dnf info`: `git`, `golang`, `make`, `gcc`, `pkgconf-pkg-config`, `alsa-lib-devel`, `pipewire`, `pipewire-alsa`, `pipewire-pulseaudio`, `python3`, `python3-numpy`, and `python3-scipy`.
