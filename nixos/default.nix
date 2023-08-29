{flake}: {
  pkgs,
  config,
  lib,
  ...
}: let
  serviceSubmodule = with lib; {
    options = {
      authKeyPath = mkOption {
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

      readHeaderTimeout = mkOption {
        description = "";
        type = types.nullOr types.str;
        default = null;
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
  };

  serviceArgs = {
    name,
    service,
  }: let
    readHeaderTimeout =
      if service.readHeaderTimeout == null
      then
        if service.funnel
        then "1s"
        else "0s"
      else service.readHeaderTimeout;
  in ([
      "-name=${name}"
      "-ephemeral=${lib.boolToString service.ephemeral}"
      "-funnel=${lib.boolToString service.funnel}"
      "-plaintext=${lib.boolToString service.plaintext}"
      "-listenAddr=${service.listenAddr}"
      "-stripPrefix=${lib.boolToString service.stripPrefix}"
      "-authkeyPath=${service.authKeyPath}"
      "-insecureHTTPS=${lib.boolToString service.insecureHTTPS}"
      "-suppressTailnetDialer=${lib.boolToString service.suppressTailnetDialer}"
      "-readHeaderTimeout=${readHeaderTimeout}"
    ]
    ++ (lib.optionals (service.whoisTimeout != null) ["-whoisTimeout" service.whoisTimeout])
    ++ (lib.optionals (service.upstreamUnixAddr != null) ["-upstreamUnixAddr" service.upstreamUnixAddr])
    ++ (map (p: "-prefix=${p}") service.prefixes)
    ++ (map (h: "-upstreamHeader=${h}") (lib.mapAttrsToList (name: service: "${name}: ${service}") service.upstreamHeaders))
    ++ [service.toURL]);
in {
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
      type = types.attrsOf (types.submodule serviceSubmodule);
      example = false;
    };

    virtualisation.oci-sidecars.tsnsrv = {
      enable = mkEnableOption "tsnsrv oci sidecar containers";

      authKeyPath = mkOption {
        description = "Path to a file containing a tailscale auth key. Make this a secret";
        type = types.path;
        default = config.services.tsnsrv.defaults.authKeyPath;
      };

      containers = mkOption {
        description = "Attrset mapping sidecar container names to their respective tsnsrv service definition. Each sidecar container will be attached to the container it belongs to, sharing its network.";
        type = types.attrsOf (types.submodule {
          options = {
            forContainer = mkOption {
              description = "The container to which to attach the sidecar.";
              type = types.str; # TODO: see if we can constrain this to all the oci containers in the system definition, with types.oneOf or an appropriate check.
            };

            service = mkOption {
              description = "tsnsrv service definition for the sidecar.";
              type = types.submodule serviceSubmodule;
            };
          };
        });
      };
    };
  };

  config = let
    lockedDownserviceConfig = {
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
      ProtectProc = "noaccess";
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
  in
    lib.mkMerge [
      (lib.mkIf config.services.tsnsrv.enable {
        users.groups.tsnsrv = {};
        systemd.services =
          lib.mapAttrs' (
            name: service:
              lib.nameValuePair
              "tsnsrv-${name}"
              {
                wantedBy = ["multi-user.target"];
                after = ["network-online.target"];
                script = ''
                  exec ${service.package}/bin/tsnsrv -stateDir=$STATE_DIRECTORY/tsnet-tsnsrv ${lib.escapeShellArgs (serviceArgs {inherit name service;})}
                '';
                serviceConfig =
                  {
                    DynamicUser = true;
                    SupplementaryGroups = [config.users.groups.tsnsrv.name] ++ service.supplementalGroups;
                    StateDirectory = "tsnsrv-${name}";
                    StateDirectoryMode = "0700";
                  }
                  // lockedDownserviceConfig;
              }
          )
          config.services.tsnsrv.services;
      })

      (lib.mkIf config.virtualisation.oci-sidecars.tsnsrv.enable {
        virtualisation.oci-containers.containers =
          lib.mapAttrs' (name: sidecar: {
            inherit name;
            value = let
              serviceName = "${config.virtualisation.oci-containers.backend}-${name}";
            in {
              imageFile = flake.packages.${pkgs.stdenv.targetPlatform.system}.tsnsrvOciImage;
              image = "tsnsrv:latest";
              dependsOn = [sidecar.forContainer];
              volumes = [
                # The service's state dir; we have to infer /var/lib
                # because the backends don't support using the
                # $STATE_DIRECTORY environment variable in volume specs.
                "/var/lib/${serviceName}:/state"

                # The tsnet auth key.
                "${config.virtualisation.oci-sidecars.tsnsrv.authKeyPath}:${config.virtualisation.oci-sidecars.tsnsrv.authKeyPath}"
              ];
              extraOptions = [
                "--network=container:${sidecar.forContainer}"
              ];
              cmd =
                ["-stateDir=/state"]
                ++ (serviceArgs {
                  inherit name;
                  inherit (sidecar) service;
                });
            };
          })
          config.virtualisation.oci-sidecars.tsnsrv.containers;

        systemd.services =
          (
            # systemd unit settings for the respective podman services:
            lib.mapAttrs' (name: sidecar: let
              serviceName = "${config.virtualisation.oci-containers.backend}-${name}";
              service = sidecar.service;
            in {
              name = serviceName;
              value = {
                serviceConfig =
                  {
                    StateDirectory = serviceName;
                    StateDirectoryMode = "0700";
                    DynamicUser = true;
                    SupplementaryGroups = [config.users.groups.tsnsrv.name] ++ service.supplementalGroups;
                  }
                  // lockedDownserviceConfig;
              };
            })
            config.virtualisation.oci-sidecars.tsnsrv.containers
          )
          // (
            # systemd unit of the container we're sidecar-ing to:
            # Ensure that the sidecar is up when the "main" container is up.
            lib.foldAttrs (item: acc: {unitConfig.Upholds = acc.unitConfig.Upholds ++ [item];})
            {unitConfig.Upholds = [];}
            (lib.mapAttrsToList (name: sidecar: let
                fromServiceName = "${config.virtualisation.oci-containers.backend}-${sidecar.forContainer}";
                toServiceName = "${config.virtualisation.oci-containers.backend}-${name}";
              in {
                "${fromServiceName}" = toServiceName;
              })
              config.virtualisation.oci-sidecars.tsnsrv.containers)
          );
      })
    ];
}
