# Fedora 44 troubleshooting

Check the user audio services:

```sh
systemctl --user status pipewire
systemctl --user status wireplumber
```

List PipeWire devices and sinks:

```sh
wpctl status
```

Confirm the ALSA compatibility devices used by Oto:

```sh
aplay -L
```

If compilation reports ALSA/pkg-config errors, install `gcc pkgconf-pkg-config alsa-lib-devel`. A real-time Linux binary needs `CGO_ENABLED=1`; `go env CGO_ENABLED` should print `1`.

No physical sink is required for deterministic operation:

```sh
./out/lgs repl --no-audio
./out/lgs doctor --no-audio
./out/lgs render --input testdata/programs/single-note.lg \
  --output out/a4.wav --duration 2s
```

`doctor` treats an unavailable real-time device as optional and exits successfully when let-go, Sointu, WAV generation, and analysis work. If real-time startup fails while `wpctl` shows a sink, verify that `pipewire-alsa` is installed and that the current user owns an active login session.
