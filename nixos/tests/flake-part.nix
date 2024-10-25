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
        importTests = dir:
          lib.mapAttrs' (name: value: {
            name = "${dir}/${name}";
            inherit value;
          }) (import ./${dir} {
            inherit pkgs nixos-lib;
            nixosModule = self.nixosModules.default;
            validatorPackage = config.packages.tsnsrvCmdLineValidator;
          });
      in
        lib.mkMerge [
          (importTests "cmdline")
          (importTests "e2e")
        ];
  };
}
