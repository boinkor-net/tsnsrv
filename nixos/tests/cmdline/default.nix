# Tests that ensure that generated commandline arguments are correct
# (they use the tsnsrvCmdLineValidator package output, which has all
# functionality stubbed out).
{
  pkgs,
  nixos-lib,
  nixosModule,
  validatorPackage,
}: {
  all = import ./all.nix {inherit pkgs nixos-lib nixosModule validatorPackage;};
}
