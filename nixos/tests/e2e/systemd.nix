{
  pkgs,
  nixos-lib,
  nixosModule,
}: let
  stunPort = 3478;
in
  nixos-lib.runTest {
    name = "systemd";
    hostPkgs = pkgs;

    defaults.services.tsnsrv.enable = true;
    defaults.services.tsnsrv.defaults.tsnetVerbose = true;

    nodes.machine = {
      config,
      pkgs,
      lib,
      ...
    }: {
      imports = [
        nixosModule
      ];

      environment.systemPackages = [
        pkgs.headscale
        pkgs.tailscale
        (pkgs.writeShellApplication {
          name = "tailscale-up-for-tests";
          text = ''
            tailscale up \
              --login-server=${config.services.headscale.settings.server_url} \
              --auth-key="$(cat /run/ts-authkey)"
          '';
        })
      ];
      virtualisation.cores = 4;
      virtualisation.memorySize = 1024;
      services.headscale = {
        enable = true;
        settings = {
          ip_prefixes = ["100.64.0.0/10"];
          dns.magic_dns = false;
          dns.override_local_dns = false;
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

      services.static-web-server = {
        enable = true;
        listen = "127.0.0.1:3000";
        root = pkgs.writeTextDir "index.html" "It works!";
      };
      services.tsnsrv = {
        defaults.urlParts.host = "127.0.0.1";
        defaults.loginServerUrl = config.services.headscale.settings.server_url;
        defaults.authKeyPath = "/run/ts-authkey";
        services.basic = {
          timeout = "10s";
          listenAddr = ":80";
          plaintext = true; # HTTPS requires certs
          toURL = "http://127.0.0.1:3000";
        };
        services.urlparts = {
          timeout = "10s";
          listenAddr = ":80";
          plaintext = true; # HTTPS requires certs
          urlParts.port = 3000;
        };
      };
      systemd.services.tsnsrv-basic = {
        enableStrictShellChecks = true;
        unitConfig.ConditionPathExists = config.services.tsnsrv.services.basic.authKeyPath;
      };
      systemd.services.tsnsrv-urlparts = {
        enableStrictShellChecks = true;
        unitConfig.ConditionPathExists = config.services.tsnsrv.services.basic.authKeyPath;
      };
    };

    testScript = ''
      machine.start()
      machine.wait_for_unit("tailscaled.service", timeout=30)
      machine.wait_for_unit("headscale.service", timeout=30)
      machine.wait_until_succeeds("headscale users list", timeout=90)
      machine.succeed("headscale users create machine")
      machine.succeed("headscale preauthkeys create --reusable -e 24h -u machine > /run/ts-authkey")
      machine.succeed("tailscale-up-for-tests", timeout=30)
      import time
      import json

      def wait_for_tsnsrv_registered(name):
          "Poll until tsnsrv appears in the list of hosts, then return its IP."
          while True:
              output = json.loads(machine.succeed("headscale nodes list -o json-line"))
              basic_entry = [elt["ip_addresses"][0] for elt in output if elt["given_name"] == name]
              if len(basic_entry) == 1:
                  return basic_entry[0]
              time.sleep(1)

      def test_script_e2e(name):
          @polling_condition
          def tsnsrv_running():
              machine.succeed(f"systemctl is-active tsnsrv-{name}")

          machine.wait_until_succeeds("headscale nodes list -o json-line")
          machine.systemctl(f"start tsnsrv-{name}")
          machine.wait_for_unit(f"tsnsrv-{name}", timeout=30)
          with tsnsrv_running:
              # We don't have magic DNS in this setup, so let's figure out the IP from the node list:
              tsnsrv_ip = wait_for_tsnsrv_registered(name)
              print(f"tsnsrv-{name} seems up, with IP {tsnsrv_ip}")
              machine.wait_until_succeeds(f"tailscale ping {tsnsrv_ip}", timeout=30)
              print(machine.succeed(f"curl -f http://{tsnsrv_ip}"))
      test_script_e2e(name="basic")
      test_script_e2e(name="urlparts")
    '';
  }
