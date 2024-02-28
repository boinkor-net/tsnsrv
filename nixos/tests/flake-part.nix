# Tests for the nixos module. Intended to be invoked via & merged into
# the flake's check attribute.
{
  self,
  lib,
  pkgs',
  inputs,
  withSystem,
  ...
}: {
  perSystem = {
    config,
    pkgs,
    final,
    system,
    ...
  }: {
    checks = let
      nixos-lib = import "${inputs.nixpkgs}/nixos/lib" {};
    in
      if ! pkgs.lib.hasSuffix "linux" system
      then {}
      else let
        stunPort = 3478;
        e2eTest = {
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
                self.nixosModules.default
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
              services.headscale = {
                enable = true;
                settings = {
                  ip_prefixes = ["100.64.0.0/10"];
                  derp.server = {
                    enabled = true;
                    region_id = 999;
                    stun_listen_addr = "0.0.0.0:${toString stunPort}";
                  };
                };
              };
              services.tailscale.enable = true;
              networking.firewall = {
                allowedTCPPorts = [80 443];
                allowedUDPPorts = [stunPort];
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
                      headscale preauthkeys create --reusable -e 24h -u "$1" > "$STATE_DIRECTORY"/"$1".preauth-key
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
              machine.wait_for_unit("tailscaled.service", timeout=10)
              machine.succeed("tailscale-up-for-tests", timeout=10)
              ${testScript}
            '';
          };

        cmdLineValidation = {
          testConfig,
          testScript,
        }:
          nixos-lib.runTest {
            name = "tsnsrv-nixos";
            hostPkgs = pkgs;

            defaults.services.tsnsrv.enable = true;
            defaults.services.tsnsrv.defaults.package = config.packages.tsnsrvCmdLineValidator;
            defaults.services.tsnsrv.defaults.authKeyPath = "/dev/null";

            nodes.machine = {...}: {
              imports = [self.nixosModules.default testConfig];

              virtualisation.cores = 4;
              virtualisation.memorySize = 1024;
            };

            testScript = ''
              machine.start()
              ${testScript}
            '';
          };
      in {
        cmdline-basic = cmdLineValidation {
          testConfig = {
            services.tsnsrv.services.basic.toURL = "http://127.0.0.1:3000";
          };
          testScript = ''
            machine.wait_for_unit("tsnsrv-basic")
          '';
        };

        cmdline-with-custom-certs = cmdLineValidation {
          testConfig = {
            services.tsnsrv.services.custom = {
              toURL = "http://127.0.0.1:3000";
              certificateFile = "/tmp/cert.pem";
              certificateKey = "/tmp/key.pem";
            };
          };
          testScript = ''
            machine.wait_for_unit("tsnsrv-custom")
          '';
        };

        e2e-plaintext = e2eTest {
          testConfig = {
            config,
            pkgs,
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
            machine.wait_until_succeeds("headscale nodes list -o json-line")

            # We don't have magic DNS in this setup, so let's figure out the IP from the node list:
            output = json.loads(machine.succeed("headscale nodes list -o json-line"))
            tsnsrv_ip = [elt["ip_addresses"][0] for elt in output if elt["given_name"] == "basic"][0]
            print(f"tsnsrv seems up, with IP {tsnsrv_ip}")
            machine.wait_until_succeeds(f"tailscale ping {tsnsrv_ip}", timeout=10)
            print(machine.succeed(f"curl -f http://{tsnsrv_ip}"))
          '';
        };
      };
  };
}
