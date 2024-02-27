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
        tsnsrvPkg = p: subPackage:
          p.buildGo122Module {
            pname = builtins.baseNameOf subPackage;
            version = "0.0.0";
            vendorHash = builtins.readFile ./tsnsrv.sri;
            src = with p; lib.sourceFilesBySuffices (lib.sources.cleanSource ./.) [".go" ".mod" ".sum"];
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

        checks = let
          nixos-lib = import "${nixpkgs}/nixos/lib" {};
        in
          if ! pkgs.lib.hasSuffix "linux" system
          then {}
          else let
            cmdLineValidation = {
              testConfig,
              testScript,
            }:
              nixos-lib.runTest {
                name = "tsnsrv-nixos";
                hostPkgs = pkgs;

                defaults.services.tsnsrv.enable = true;
                defaults.services.tsnsrv.defaults.tsnetVerbose = true;

                # defaults.services.tsnsrv.defaults.package = config.packages.tsnsrvCmdLineValidator;

                nodes.machine = {
                  config,
                  pkgs,
                  lib,
                  ...
                }: {
                  imports = [
                    (import ./nixos {flake = self;})
                    testConfig
                  ];

                  environment.systemPackages = [
                    pkgs.headscale
                    pkgs.tailscale
                    (pkgs.writeShellApplication {
                      name = "tailscale-up-for-tests";
                      text = ''
                        systemctl start --wait generate-tsnsrv-authkey@tailscaled.service
                        tailscale up \
                          --login-server=${config.services.headscale.settings.server_url} \
                          --auth-key="$(cat /var/lib/headscale-authkeys/tailscaled.preauth-key)"
                      '';
                    })
                  ];
                  virtualisation.cores = 4;
                  virtualisation.memorySize = 1024;
                  services.headscale.enable = true;
                  services.tailscale = {
                    enable = true;
                  };
                  systemd.services."generate-tsnsrv-authkey@" = {
                    description = "Generate headscale authkey for %i";
                    serviceConfig.ExecStart = let
                      startScript = pkgs.writeShellApplication {
                        name = "generate-tsnsrv-authkey";
                        runtimeInputs = [pkgs.headscale pkgs.jq];
                        text = ''
                          set -x
                          headscale users create "$1"
                          headscale preauthkeys create --reusable -e 24h -u "$1" | tail -n1 > "$STATE_DIRECTORY"/"$1".preauth-key
                          echo generated "$STATE_DIRECTORY"/"$1".preauth-key
                          cat "$STATE_DIRECTORY"/"$1".preauth-key
                        '';
                      };
                    in "${lib.getExe startScript} %i";
                    wants = ["headscale.service"];
                    after = ["headscale.service"];
                    serviceConfig.Type = "oneshot";
                    serviceConfig.StateDirectory = "headscale-authkeys";
                    serviceConfig.Group = "tsnsrv";
                    unitConfig.Requires = ["headscale.service"];
                  };
                };

                testScript = ''
                  machine.start()
                  machine.wait_for_unit("tailscaled.service")
                  machine.succeed("tailscale-up-for-tests")
                  ${testScript}
                '';
              };
          in {
            nixos-basic = cmdLineValidation {
              testConfig = {
                config,
                pkgs,
                lib,
                ...
              }: {
                systemd.services.tsnsrv-basic = {
                  wants = ["generate-tsnsrv-authkey@basic.service"];
                  after = ["generate-tsnsrv-authkey@basic.service"];
                  unitConfig.Requires = ["generate-tsnsrv-authkey@basic.service"];
                };
                services.static-web-server = {
                  enable = true;
                  listen = "127.0.0.1:3000";
                  root = pkgs.writeTextDir "index.html" "It works!";
                };
                services.tsnsrv = {
                  defaults.loginServerUrl = config.services.headscale.settings.server_url;
                  defaults.authKeyPath = "/var/lib/headscale-authkeys/basic.preauth-key";
                  services.basic = {
                    timeout = "10s";
                    listenAddr = ":80";
                    plaintext = true; # HTTPS requires certs
                    toURL = "http://127.0.0.1:3000";
                  };
                };
              };
              testScript = ''
                import json

                # TODO: Once https://github.com/juanfont/headscale/issues/1797 is fixed, that defensive grep can go away.
                machine.wait_until_succeeds("headscale nodes list -o json-line | grep '^\[.*basic'")

                # We don't have magic DNS in this setup, so let's figure out the IP from the node list:
                output = json.loads(machine.succeed("headscale nodes list -o json-line | grep '^\['"))
                tsnsrv_ip = [elt["ip_addresses"][0] for elt in output if elt["given_name"] == "basic"][0]
                print(f"tsnsrv seems up, with IP {tsnsrv_ip}")
                machine.wait_until_succeeds(f"tailscale ping {tsnsrv_ip}", timeout=10)
                machine.succeed(f"curl -f http://{tsnsrv_ip}")
              '';
            };
          };

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
