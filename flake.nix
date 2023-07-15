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
            vendorHash = "sha256-hxfMKhnJ13lMqD+mSQXEBFd2j63w/lEif5eGfS3OjkA="; # TODO: use nardump to generate this
            src = ./.;
          };
          default = config.packages.tsnsrv;
        };
        formatter = pkgs.alejandra;

        devshells.default = {
          packages = [
            pkgs.go
            pkgs.gopls
          ];
        };
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
