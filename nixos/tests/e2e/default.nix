{
  pkgs,
  nixos-lib,
  nixosModule,
  ...
}: {
  systemd = import ./systemd.nix {inherit pkgs nixos-lib nixosModule;};
  systemd-urlparts = import ./systemd-urlparts.nix {inherit pkgs nixos-lib nixosModule;};
  oci = import ./oci.nix {inherit pkgs nixos-lib nixosModule;};
  oci-urlparts = import ./oci-urlparts.nix {inherit pkgs nixos-lib nixosModule;};
}
