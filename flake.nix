{
  outputs = inputs @ {
    self,
    flake-parts,
    ...
  }:
    flake-parts.lib.mkFlake {inherit inputs;} {
      imports = [
        inputs.flake-parts.flakeModules.easyOverlay
        inputs.flake-parts.flakeModules.partitions
        ./nixos/tests/flake-part.nix
      ];
      systems = [
        "x86_64-darwin"
        "x86_64-linux"
        "aarch64-darwin"
        "aarch64-linux"
      ];
      perSystem = {
        config,
        lib,
        pkgs,
        system,
        ...
      }: let
        tsnsrvPkg = p: subPackage:
          p.buildGo123Module {
            pname = builtins.baseNameOf subPackage;
            version = "0.0.0";
            vendorHash = builtins.readFile ./tsnsrv.sri;
            src = lib.sourceFilesBySuffices (lib.sources.cleanSource ./.) [".go" ".mod" ".sum"];
            subPackages = [subPackage];
            ldflags = ["-s" "-w"];
            meta.mainProgram = builtins.baseNameOf subPackage;
          };
        imageArgs = p: {
          name = "tsnsrv";
          tag = "latest";
          contents = [
            (p.buildEnv {
              name = "image-root";
              paths = [(tsnsrvPkg p "cmd/tsnsrv")];
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
          tsnsrv = tsnsrvPkg pkgs "cmd/tsnsrv";
          tsnsrvCmdLineValidator = tsnsrvPkg pkgs "cmd/tsnsrvCmdLineValidator";

          # This platform's "natively" built docker image:
          tsnsrvOciImage = pkgs.dockerTools.buildLayeredImage (imageArgs pkgs);

          # "cross-platform" build, mainly to support building on github actions (but also on macOS with apple silicon):
          tsnsrvOciImage-cross-aarch64-linux = pkgs.pkgsCross.aarch64-multiplatform.dockerTools.buildLayeredImage (imageArgs pkgs.pkgsCross.aarch64-multiplatform);
        };

        formatter = pkgs.alejandra;
      };

      partitionedAttrs = {
        checks = "dev";
        devShells = "dev";
        apps = "dev";
      };
      partitions.dev = {
        extraInputsFlake = ./dev;
        module = ./dev/flake-part.nix;
      };
      flake = {
        nixosModules = {
          default = import ./nixos {flake = self;};
        };
      };
    };

  inputs = {
    flake-parts.url = "github:hercules-ci/flake-parts";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };
}
