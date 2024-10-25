# Tests that ensure that generated commandline arguments are correct
# (they use the tsnsrvCmdLineValidator package output, which has all
# functionality stubbed out).
{
  pkgs,
  nixos-lib,
  nixosModule,
  validatorPackage,
}: {
  basic = import ./basic.nix {inherit pkgs nixos-lib nixosModule validatorPackage;};
  with-custom-certs = import ./with-custom-certs.nix {inherit pkgs nixos-lib nixosModule validatorPackage;};
}
