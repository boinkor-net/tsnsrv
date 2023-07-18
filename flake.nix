{
  outputs = inputs @ {
    flake-parts,
    nixpkgs,
    ...
  }:
    flake-parts.lib.mkFlake {inherit inputs;} {
      imports = [
        inputs.devshell.flakeModule
        inputs.flake-parts.flakeModules.easyOverlay
      ];
      systems = [
        "x86_64-darwin"
        "x86_64-linux"
        "aarch64-darwin"
        "aarch64-linux"
      ];
      perSystem = {
        config,
        pkgs,
        final,
        ...
      }: {
        overlayAttrs = {
          inherit (config.packages) tsnsrv;
        };

        packages = {
          tsnsrv = pkgs.buildGoModule {
            pname = "tsnsrv";
            version = "0.0.0";
            vendorHash = builtins.readFile ./tsnsrv.sri;
            src = ./.;
            meta.mainProgram = "tsnsrv";
          };

          # To provide a smoother dev experience:
          nardump = pkgs.buildGoModule rec {
            pname = "nardump";
            version = "1.38.4";
            src = pkgs.fetchFromGitHub {
              owner = "tailscale";
              repo = "tailscale";
              rev = "v${version}";
              sha256 = "sha256-HjN8VzysxQvx5spXgbgbItH3y1bLbfHO+udNQMuyhAk=";
            };
            vendorSha256 = "sha256-LIvaxSo+4LuHUk8DIZ27IaRQwaDnjW6Jwm5AEc/V95A=";

            subPackages = ["cmd/nardump"];
          };
          default = config.packages.tsnsrv;
        };

        apps = {
          tsnsrv.program = config.packages.tsnsrv;
          default = config.apps.tsnsrv;
        };
        formatter = pkgs.alejandra;

        devshells.default = {
          commands = [
            {
              name = "regenSRI";
              category = "dev";
              help = "Regenerate tsnsrv.sri in case the module SRI hash should change";
              command = ''
                set -eu -o pipefail

                src="$(pwd)"
                temp="$(mktemp -d)"
                trap "rm -rf $temp" EXIT
                go mod vendor -o "$temp"
                ${config.packages.nardump}/bin/nardump -sri $temp >"$src/tsnsrv.sri"
              '';
            }
          ];
          packages = [
            pkgs.go
            pkgs.gopls
          ];
        };
      };

      flake.nixosModules = {
        default = {imports = [./nixos];};
      };
    };

  inputs = {
    flake-parts.url = "github:hercules-ci/flake-parts";
    devshell.url = "github:numtide/devshell";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-compat = {
      url = "github:edolstra/flake-compat";
      flake = false;
    };
  };
}
