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
    testConfig = {
      services.tsnsrv.services.basic.toURL = "http://127.0.0.1:3000";
    };
    testScript = ''
      machine.wait_for_unit("tsnsrv-basic")
    '';
  }
