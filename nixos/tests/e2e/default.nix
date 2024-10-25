{
  pkgs,
  nixos-lib,
  nixosModule,
  ...
}: {
  systemd = import ./systemd.nix {inherit pkgs nixos-lib nixosModule;};
}
