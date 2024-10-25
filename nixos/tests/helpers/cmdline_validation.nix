{
  pkgs,
  nixos-lib,
  nixosModule,
  validatorPackage,
}: {
  testConfig,
  testScript,
}:
nixos-lib.runTest {
  name = "tsnsrv-nixos";
  hostPkgs = pkgs;

  defaults.services.tsnsrv.enable = true;
  defaults.services.tsnsrv.defaults.package = validatorPackage;
  defaults.services.tsnsrv.defaults.authKeyPath = "/dev/null";

  nodes.machine = {...}: {
    imports = [
      nixosModule
      testConfig
    ];

    virtualisation.cores = 4;
    virtualisation.memorySize = 1024;
  };

  testScript = ''
    machine.start()
    ${testScript}
  '';
}
