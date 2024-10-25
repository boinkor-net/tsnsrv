{
  pkgs,
  nixos-lib,
  nixosModule,
  ...
}: {
  systemd = import ./systemd.nix {inherit pkgs nixos-lib nixosModule;};
  oci = import ./oci.nix {inherit pkgs nixos-lib nixosModule;};
}
