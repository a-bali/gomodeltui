{
  description = "Read-only GoModel metrics and live logs TUI";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in {
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [ go gopls golangci-lint goreleaser ];
          shellHook = ''
            export GOPATH="''${GOPATH:-$PWD/.gopath}"
            export GOTOOLCHAIN="''${GOTOOLCHAIN:-local}"
            echo "gomodeltui development shell"
          '';
        };
      });
    };
}
