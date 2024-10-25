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
        e2eTest = import ./helpers/e2e_test.nix {
          inherit pkgs nixos-lib;
          nixosModule = self.nixosModules.default;
        };

        cmdLineValidation = import ./helpers/cmdline_validation.nix {
          inherit pkgs nixos-lib;
          nixosModule = self.nixosModules.default;
          package = config.packages.tsnsrvCmdLineValidator;
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
            import time
            import json

            @polling_condition
            def tsnsrv_running():
                machine.succeed("systemctl is-active tsnsrv-basic")

            def wait_for_tsnsrv_up():
                "Poll until tsnsrv appears in the list of hosts, then return its IP."
                while True:
                    output = json.loads(machine.succeed("headscale nodes list -o json-line"))
                    basic_entry = [elt["ip_addresses"][0] for elt in output if elt["given_name"] == "basic"]
                    if len(basic_entry) == 1:
                        return basic_entry[0]
                    time.sleep(1)

            def test_script_e2e():
                machine.wait_until_succeeds("headscale nodes list -o json-line")
                machine.wait_for_unit("tsnsrv-basic", timeout=30)
                with tsnsrv_running:
                    # We don't have magic DNS in this setup, so let's figure out the IP from the node list:
                    tsnsrv_ip = wait_for_tsnsrv_up()
                    print(f"tsnsrv seems up, with IP {tsnsrv_ip}")
                    machine.wait_until_succeeds(f"tailscale ping {tsnsrv_ip}", timeout=30)
                    print(machine.succeed(f"curl -f http://{tsnsrv_ip}"))
            test_script_e2e()
          '';
        };
      };
  };
}
