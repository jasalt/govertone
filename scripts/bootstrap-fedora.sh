#!/usr/bin/env bash
set -euo pipefail
packages=(git golang make gcc pkgconf-pkg-config alsa-lib-devel pipewire pipewire-alsa pipewire-pulseaudio python3 python3-numpy python3-scipy)
for package in "${packages[@]}"; do
  dnf info "$package" >/dev/null || { echo "Fedora package not found: $package" >&2; exit 1; }
done
sudo dnf install -y "${packages[@]}"
go version
