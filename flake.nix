{
  description = "lgs: let-go + Sointu music runtime (development shell)";

  # Development shell only; `lgs` is still built with `make build`.
  #
  #   nix develop                  # enter the shell
  #   nix develop -c make acceptance
  #
  # UPSTREAM DEPENDENCY (#27): let-go v1.13.0+ requires Go 1.27, and the
  # let-go `*audio*` sink needs let-go v1.14+, which is not tagged yet.
  # When upgrading let-go, change `go` below to `pkgs.go_1_27` together with
  # the `go` directive in go.mod, the CI go-version and docs/dependencies.md.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in {
      devShells = forAll (pkgs:
        let
          go = pkgs.go_1_26;
          python = pkgs.python3.withPackages (p: [ p.numpy p.scipy ]);
        in {
          default = pkgs.mkShell {
            packages = [ go pkgs.gnumake pkgs.git pkgs.pkg-config python ]
              # Real-time output (Oto) needs cgo; ALSA only exists on Linux.
              ++ pkgs.lib.optionals pkgs.stdenv.isLinux [ pkgs.gcc pkgs.alsa-lib ];
            # Use the Nix Go, never a toolchain downloaded by `go`.
            GOTOOLCHAIN = "local";
            CGO_ENABLED = "1";
            shellHook = ''
              echo "lgs dev shell: $(go version | cut -d' ' -f3-4); run 'make acceptance'"
            '';
          };
        });
    };
}
