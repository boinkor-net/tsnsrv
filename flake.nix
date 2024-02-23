{
  outputs = inputs @ {
    self,
    flake-parts,
    flocken,
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
        system,
        ...
      }: let
        tsnsrvPkg = p:
          p.buildGo122Module {
            pname = "tsnsrv";
            version = "0.0.0";
            vendorHash = builtins.readFile ./tsnsrv.sri;
            src = with p; lib.sourceFilesBySuffices (lib.sources.cleanSource ./.) [".go" ".mod" ".sum"];
            ldflags = ["-s" "-w"];
            meta.mainProgram = "tsnsrv";
          };
        imageArgs = p: {
          name = "tsnsrv";
          tag = "latest";
          contents = [
            (p.buildEnv {
              name = "image-root";
              paths = [(tsnsrvPkg p)];
              pathsToLink = ["/bin" "/tmp"];
            })
            p.dockerTools.caCertificates
          ];

          config.EntryPoint = ["/bin/tsnsrv"];
        };
      in {
        overlayAttrs = {
          inherit (config.packages) tsnsrv tsnsrvOciImage;
        };

        packages = {
          default = config.packages.tsnsrv;
          tsnsrv = tsnsrvPkg pkgs;

          # This platform's "natively" built docker image:
          tsnsrvOciImage = pkgs.dockerTools.buildLayeredImage (imageArgs pkgs);

          # "cross-platform" build, mainly to support building on github actions (but also on macOS with apple silicon):
          tsnsrvOciImage-cross-aarch64-linux = pkgs.pkgsCross.aarch64-multiplatform.dockerTools.buildLayeredImage (imageArgs pkgs.pkgsCross.aarch64-multiplatform);

          # To provide a smoother dev experience:
          regenSRI = let
            nardump = pkgs.buildGoModule rec {
              pname = "nardump";
              version = "1.38.4";
              src = pkgs.fetchFromGitHub {
                owner = "tailscale";
                repo = "tailscale";
                rev = "v${version}";
                sha256 = "sha256-HjN8VzysxQvx5spXgbgbItH3y1bLbfHO+udNQMuyhAk=";
              };
              vendorHash = "sha256-LIvaxSo+4LuHUk8DIZ27IaRQwaDnjW6Jwm5AEc/V95A=";

              subPackages = ["cmd/nardump"];
            };
          in
            pkgs.writeShellApplication {
              name = "regenSRI";
              text = ''
                set -eu -o pipefail

                src="$(pwd)"
                temp="$(mktemp -d)"
                trap 'rm -rf "$temp"' EXIT
                go mod vendor -o "$temp"
                ${nardump}/bin/nardump -sri "$temp" >"$src/tsnsrv.sri"
              '';
            };
        };

        apps = {
          default = config.apps.tsnsrv;
          tsnsrv.program = config.packages.tsnsrv;
          streamTsnsrvOciImage.program = "${pkgs.dockerTools.streamLayeredImage imageArgs}";

          pushImagesToGhcr = {
            program = flocken.legacyPackages.${system}.mkDockerManifest (let
              ref = builtins.getEnv "GITHUB_REF_NAME";
              branch =
                if pkgs.lib.hasSuffix "/merge" ref
                then "pr-${pkgs.lib.removeSuffix "/merge" ref}"
                else ref;
            in {
              inherit branch;
              name = "ghcr.io/" + builtins.getEnv "GITHUB_REPOSITORY";
              version = builtins.getEnv "VERSION";

              # Here we build the x86_64-linux variants only because
              # that is what runs on GHA, whence we push the images to
              # ghcr.
              images = with self.packages; [
                x86_64-linux.tsnsrvOciImage
                x86_64-linux.tsnsrvOciImage-cross-aarch64-linux
              ];
            });
            type = "app";
          };
        };
        formatter = pkgs.alejandra;

        devshells.default = {
          commands = [
            {
              name = "regenSRI";
              category = "dev";
              help = "Regenerate tsnsrv.sri in case the module SRI hash should change";
              command = "${config.packages.regenSRI}/bin/regenSRI";
            }
          ];
          packages = [
            pkgs.go_1_22
            pkgs.gopls
            (pkgs.golangci-lint.override
              {buildGoModule = args: (pkgs.buildGo122Module args);})
          ];
        };
      };

      flake.nixosModules = {
        default = import ./nixos {flake = self;};
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
    flocken = {
      url = "github:mirkolenz/flocken/v1";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };
}
