{flake}: {
  pkgs,
  config,
  lib,
  ...
}: {
  options = with lib; {
    services.tsnsrv.enable = mkOption {
      description = "Enable tsnsrv";
      type = types.bool;
      default = false;
    };

    services.tsnsrv.package = mkOption {
      description = "Package to run tsnsrv out of";
      default = flake.packages.${pkgs.stdenv.targetPlatform.system}.tsnsrv;
      type = types.package;
    };

    services.tsnsrv.services = mkOption {
      description = "tsnsrv services";
      default = {};
      type = types.attrsOf (types.submodule {
        options = {
          ephemeral = mkOption {
            description = "Delete the tailnet participant shortly after it goes offline";
            type = types.bool;
            default = false;
          };

          funnel = mkOption {
            description = "Serve HTTP as a funnel, meaning that it is available on the public internet.";
            type = types.bool;
            default = false;
          };

          listenAddr = mkOption {
            description = "Address to listen on";
            type = types.str;
            default = ":443";
          };

          plaintext = mkOption {
            description = "Whether to serve non-TLS-encrypted plaintext HTTP";
            type = types.bool;
            default = false;
          };

          downstreamUnixAddr = mkOption {
            description = "Connect only to the given UNIX Domain Socket";
            type = types.nullOr types.path;
            default = null;
          };

          prefixes = mkOption {
            description = "URL path prefixes to allow in forwarding. Acts as an allowlist but if unset, all prefixes are allowed.";
            type = types.listOf types.str;
            default = [];
          };

          stripPrefix = mkOption {
            description = "Strip matched prefix from request to upstream. Probably should be true when allowlisting multiple prefixes.";
            type = types.bool;
            default = true;
          };

          toURL = mkOption {
            description = "URL to forward HTTP requests to";
            type = types.str;
          };

          supplementalGroups = mkOption {
            description = "List of groups to run the service under (in addition to the 'tsnsrv' group)";
            type = types.listOf types.str;
            default = [];
          };
        };
      });
      example = false;
    };

    services.tsnsrv.authKeyPath = lib.mkOption {
      description = "Path to a file containing a tailscale auth key. Make this a secret";
    };
  };

  config = lib.mkIf config.services.tsnsrv.enable (let
    toBool = val:
      if val == true
      then "true"
      else "false";
  in {
    users.groups.tsnsrv = {};
    systemd.services =
      lib.mapAttrs' (
        name: value:
          lib.nameValuePair
          "tsnsrv-${name}"
          {
            wantedBy = ["multi-user.target"];
            after = ["network-online.target"];
            script = ''
              export TS_AUTHKEY="$(cat ${config.services.tsnsrv.authKeyPath})"
              export XDG_CONFIG_HOME="$STATE_DIRECTORY"
              exec ${config.services.tsnsrv.package}/bin/tsnsrv -name "${name}" \
                     -ephemeral=${toBool value.ephemeral} \
                     -funnel=${toBool value.funnel} \
                     -plaintext=${toBool value.plaintext} \
                     -listenAddr="${value.listenAddr}" \
                     -stripPrefix="${toBool value.stripPrefix}" \
                     ${
                if value.downstreamUnixAddr != null
                then "-downstreamUnixAddr=${value.downstreamUnixAddr}"
                else ""
              } \
              ${
                lib.concatMapStringsSep " \\\n" (p: "-prefix \"${p}\"") value.prefixes
              } \
                     "${value.toURL}"
            '';
            serviceConfig = {
              DynamicUser = true;
              SupplementaryGroups = [config.users.groups.tsnsrv.name] ++ value.supplementalGroups;
              StateDirectory = "tsnsrv-${name}";
              StateDirectoryMode = "0700";

              PrivateNetwork = false; # We need access to the internet for ts
              # Activate a bunch of strictness:
              DeviceAllow = "";
              LockPersonality = true;
              MemoryDenyWriteExecute = true;
              NoNewPrivileges = true;
              PrivateDevices = true;
              PrivateMounts = true;
              PrivateTmp = true;
              PrivateUsers = true;
              ProtectClock = true;
              ProtectControlGroups = true;
              ProtectHome = true;
              ProtectProc = true;
              ProtectKernelModules = true;
              ProtectHostname = true;
              ProtectKernelLogs = true;
              ProtectKernelTunables = true;
              RestrictNamespaces = true;
              AmbientCapabilities = "";
              CapabilityBoundingSet = "";
              ProtectSystem = "strict";
              RemoveIPC = true;
              RestrictRealtime = true;
              RestrictSUIDSGID = true;
              UMask = "0066";
            };
          }
      )
      config.services.tsnsrv.services;
  });
}
