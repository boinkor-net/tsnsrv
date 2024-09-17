{
  self,
  inputs,
  ...
}: {
  imports = [
    inputs.devshell.flakeModule
    inputs.generate-go-sri.flakeModules.default
  ];
  systems = ["x86_64-linux" "aarch64-darwin"];

  perSystem = {
    config,
    pkgs,
    system,
    flocken,
    ...
  }: {
    go-sri-hashes.tsnsrv = {};

    devshells.default = {
      commands = [
        {
          name = "regenSRI";
          category = "dev";
          help = "Regenerate tsnsrv.sri in case the module SRI hash should change";
          command = "${config.apps.generate-sri-tsnsrv.program}";
        }
      ];
      packages = [
        pkgs.go_1_23
        pkgs.gopls
        pkgs.golangci-lint
      ];
    };

    apps = {
      default = config.apps.tsnsrv;
      tsnsrv.program = config.packages.tsnsrv;

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
  };
}
