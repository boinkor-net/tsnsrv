{
  self,
  inputs,
  ...
}:
{
  imports = [
    inputs.devshell.flakeModule
    inputs.generate-go-sri.flakeModules.default
  ];
  systems = [
    "x86_64-linux"
    "aarch64-darwin"
  ];

  perSystem =
    {
      config,
      pkgs,
      system,
      flocken,
      ...
    }:
    {
      go-sri-hashes.tsnsrv = { };

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
          pkgs.go_1_24
          pkgs.gopls
          pkgs.golangci-lint
        ];
      };

      apps = {
        default = config.apps.tsnsrv;
        tsnsrv.program = config.packages.tsnsrv;

        pushImagesToGhcr = {
          program = inputs.flocken.legacyPackages.${system}.mkDockerManifest (
            let
              ref = builtins.getEnv "GITHUB_REF_NAME";
              isPR = pkgs.lib.hasSuffix "/merge" ref;
              branch = if isPR then "pr-${pkgs.lib.removeSuffix "/merge" ref}" else ref;
            in
            {
              autoTags = {
                branch = true;
                version = true;
              };
              inherit branch;

              github = {
                enable = true;
                token = "$GH_TOKEN";
              };

              # Here we build the x86_64-linux variants only because
              # that is what runs on GHA, whence we push the images to
              # ghcr.
              images = with self.packages; [
                x86_64-linux.tsnsrvOciImage
                x86_64-linux.tsnsrvOciImage-cross-aarch64-linux
              ];
            }
          );
          type = "app";
        };
      };
    };
}
