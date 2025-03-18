{
  pkgs,
  nixos-lib,
  nixosModule,
}: let
  stunPort = 3478;
in
  nixos-lib.runTest {
    name = "oci";
    hostPkgs = pkgs;

    nodes.headscale = {
      environment.systemPackages = [pkgs.headscale];
      services.headscale = {
        enable = true;
        address = "[::]";
        settings = {
          ip_prefixes = ["100.64.0.0/10"];
          dns.magic_dns = false;
          derp.server = {
            enabled = true;
            region_id = 999;
            stun_listen_addr = "0.0.0.0:${toString stunPort}";
          };
          server_url = "http://headscale:8080";
        };
      };
      networking.firewall = {
        allowedTCPPorts = [8080 443];
        allowedUDPPorts = [stunPort];
      };
    };

    nodes.machine = {
      config,
      pkgs,
      lib,
      ...
    }: {
      imports = [
        nixosModule
      ];

      environment.systemPackages = [pkgs.tailscale];
      virtualisation.cores = 4;
      virtualisation.memorySize = 1024;
      services.tailscale.enable = true;
      systemd.services.tailscaled.serviceConfig.Environment = ["TS_NO_LOGS_NO_SUPPORT=true"];

      services.tsnsrv = {
        enable = true;
        defaults.tsnetVerbose = true;
        defaults.loginServerUrl = "http://headscale:8080";
        defaults.authKeyPath = "/run/ts-authkey";
        defaults.urlParts.host = "127.0.0.1";
      };
      virtualisation.oci-sidecars.tsnsrv = {
        enable = true;
        containers.web-server-basic = {
          name = "web-server-basic";
          forContainer = "web-server";
          service = {
            timeout = "10s";
            listenAddr = ":80";
            plaintext = true;
            toURL = "http://127.0.0.1:3000";
          };
        };
        containers.web-server-urlparts = {
          name = "web-server-urlparts";
          forContainer = "web-server";
          service = {
            timeout = "10s";
            listenAddr = ":80";
            plaintext = true;
            urlParts.port = 3000;
          };
        };
      };
      virtualisation.oci-containers = let
        htmlRoot = pkgs.writeTextDir "index.html" "It works!";
      in {
        backend = "podman";
        containers.web-server = {
          image = "web-server:latest";
          imageFile = pkgs.dockerTools.buildImage {
            name = "web-server";
            tag = "latest";
            created = "now";
            copyToRoot = pkgs.buildEnv {
              name = "image-root";
              paths = [pkgs.static-web-server htmlRoot];
              pathsToLink = ["/bin"];
            };
            config.Cmd = ["/bin/static-web-server" "--port" "3000" "--root" htmlRoot];
          };
        };
      };
      systemd.services.podman-web-server-tsnsrv-basic.enableStrictShellChecks = true;
      systemd.services.podman-web-server-tsnsrv-urlparts.enableStrictShellChecks = true;
      networking.firewall.trustedInterfaces = ["podman0"];

      # Delay starting the container machinery until we have an authkey:
      systemd.services.podman-web-server-basic.unitConfig.ConditionPathExists = "/run/ts-authkey";
      systemd.services.podman-web-server-urlparts.unitConfig.ConditionPathExists = "/run/ts-authkey";

      # Serve DNS to the podman containers, otherwise they have no idea who headscale is:
      virtualisation.podman.defaultNetwork.settings.dns_enabled = true;
      services.resolved = {
        enable = true;
      };
    };

    testScript = ''
      import time
      import json

      headscale.start()
      machine.start()

      headscale.wait_for_unit("headscale.service", timeout=30)
      headscale.wait_until_succeeds("headscale users list", timeout=90)
      headscale.succeed("headscale users create machine")
      authkey = headscale.succeed("headscale preauthkeys create --reusable -e 24h -u machine")
      with open("authkey", "w") as k:
          k.write(authkey)

      machine.copy_from_host("authkey", "/run/ts-authkey")
      machine.wait_for_unit("tailscaled.service", timeout=30)
      machine.succeed('tailscale up --login-server=http://headscale:8080 --auth-key="$(cat /run/ts-authkey)"')

      def wait_for_tsnsrv_registered(name):
          "Poll until tsnsrv appears in the list of hosts, then return its IP."
          while True:
              output = json.loads(headscale.succeed("headscale nodes list -o json-line"))
              basic_entry = [elt["ip_addresses"][0] for elt in output if elt["given_name"] == name]
              if len(basic_entry) == 1:
                  return basic_entry[0]
              time.sleep(1)

      def test_script_e2e(name):
          @polling_condition
          def tsnsrv_running():
              machine.succeed(f"systemctl is-active podman-{name}")

          headscale.wait_until_succeeds("headscale nodes list -o json-line")
          machine.systemctl(f"start podman-{name}")
          machine.wait_for_unit(f"podman-{name}", timeout=30)
          with tsnsrv_running:
              # We don't have magic DNS in this setup, so let's figure out the IP from the node list:
              tsnsrv_ip = wait_for_tsnsrv_registered(name)
              print(f"tsnsrv {name} seems up, with IP {tsnsrv_ip}")
              machine.wait_until_succeeds(f"tailscale ping {tsnsrv_ip}", timeout=30)
              print(machine.succeed(f"curl -f http://{tsnsrv_ip}"))
      test_script_e2e("web-server-basic")
      test_script_e2e("web-server-urlparts")

    '';
  }
