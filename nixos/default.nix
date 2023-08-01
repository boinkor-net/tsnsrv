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

    services.tsnsrv.defaults = {
      package = mkOption {
        description = "Package to run tsnsrv out of";
        default = flake.packages.${pkgs.stdenv.targetPlatform.system}.tsnsrv;
        type = types.package;
      };

      authKeyPath = lib.mkOption {
        description = "Path to a file containing a tailscale auth key. Make this a secret";
        type = types.path;
      };
    };

    services.tsnsrv.services = mkOption {
      description = "tsnsrv services";
      default = {};
      type = types.attrsOf (types.submodule {
        options = {
          authKeyPath = lib.mkOption {
            description = "Path to a file containing a tailscale auth key. Make this a secret";
            type = types.path;
            default = config.services.tsnsrv.defaults.authKeyPath;
          };

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

          insecureHTTPS = mkOption {
            description = "Disable TLS certificate validation for requests from upstream. Insecure.";
            type = types.bool;
            default = false;
          };

          listenAddr = mkOption {
            description = "Address to listen on";
            type = types.str;
            default = ":443";
          };

          package = mkOption {
            description = "Package to use for this tsnsrv service.";
            default = config.services.tsnsrv.defaults.package;
            type = types.package;
          };

          plaintext = mkOption {
            description = "Whether to serve non-TLS-encrypted plaintext HTTP";
            type = types.bool;
            default = false;
          };

          upstreamUnixAddr = mkOption {
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

          whoisTimeout = mkOption {
            description = "Maximum amount of time that a requestor lookup may take.";
            type = types.nullOr types.str;
            default = null;
          };

          suppressWhois = mkOption {
            description = "Disable passing requestor information to upstream service";
            type = types.bool;
            default = false;
          };

          upstreamHeaders = mkOption {
            description = "Headers to set on requests to upstream.";
            type = types.attrsOf types.str;
            default = {};
          };

          suppressTailnetDialer = mkOption {
            description = "Disable using the tsnet-provided dialer, which can sometimes cause issues hitting addresses outside the tailnet";
            type = types.bool;
            default = false;
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
  };

  config = lib.mkIf config.services.tsnsrv.enable {
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
              exec ${value.package}/bin/tsnsrv -name ${lib.escapeShellArg name} \
                     -ephemeral=${lib.boolToString value.ephemeral} \
                     -funnel=${lib.boolToString value.funnel} \
                     -plaintext=${lib.boolToString value.plaintext} \
                     -listenAddr=${lib.escapeShellArg value.listenAddr} \
                     -stripPrefix=${lib.boolToString value.stripPrefix} \
                     -stateDir="$STATE_DIRECTORY/tsnet-tsnsrv" \
                     -authkeyPath=${lib.escapeShellArg value.authKeyPath} \
                     -insecureHTTPS=${lib.boolToString value.insecureHTTPS} \
                     -suppressWhois=${lib.boolToString value.suppressWhois} \
                     -suppressTailnetDialer=${lib.boolToString value.suppressTailnetDialer} \
                     ${
                if value.whoisTimeout != null
                then "-whoisTimeout=${lib.escapeShellArg value.whoisTimeout}"
                else ""
              } \
                     ${
                if value.upstreamUnixAddr != null
                then "-upstreamUnixAddr=${lib.escapeShellArg value.upstreamUnixAddr}"
                else ""
              } \
              ${
                lib.concatMapStringsSep " \\\n" (p: "-prefix ${lib.escapeShellArg p}") value.prefixes
              } \
              ${
                lib.concatMapStringsSep " \\\n" (p: "-upstreamHeader ${lib.escapeShellArg p}") (lib.mapAttrsToList (name: value: "${name}: ${value}") value.upstreamHeaders)
              } \
                     ${lib.escapeShellArg value.toURL}
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
  };
}
