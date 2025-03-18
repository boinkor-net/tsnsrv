{
  pkgs,
  nixos-lib,
  nixosModule,
  validatorPackage,
}: let
  helper = import ./../helpers/cmdline_validation.nix {
    inherit pkgs nixos-lib nixosModule validatorPackage;
  };
in
  helper {
    name = "all";
    testConfig = {
      services.tsnsrv.services.custom = {
        toURL = "http://127.0.0.1:3000";
        certificateFile = "/tmp/cert.pem";
        certificateKey = "/tmp/key.pem";
      };

      services.tsnsrv.services.basic.toURL = "http://127.0.0.1:3000";
      systemd.services.tsnsrv-basic.enableStrictShellChecks = true;
      systemd.services.tsnsrv-custom.enableStrictShellChecks = true;
    };
    testScript = ''
      machine.wait_for_unit("tsnsrv-custom")
      machine.wait_for_unit("tsnsrv-basic")
    '';
  }
