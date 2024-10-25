{
  pkgs,
  nixos-lib,
  nixosModule,
}: {
  testConfig,
  testScript,
}: let
  stunPort = 3478;
in
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
        nixosModule
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
      systemd.services.tailscaled.serviceConfig.Environment = ["TS_NO_LOGS_NO_SUPPORT=true"];
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
      def test_script_common():
          machine.start()
          machine.wait_for_unit("tailscaled.service", timeout=30)
          machine.succeed("tailscale-up-for-tests", timeout=30)

      test_script_common()
      ${testScript}
    '';
  }
