{flake}: {
  pkgs,
  config,
  lib,
  ...
}: let
  urlPartsSubmodule.options = with lib; {
    protocol = mkOption {
      description = "URL Scheme or protocol to use for reaching the upstream service.";
      type = types.str;
      default = "http";
    };

    host = mkOption {
      description = "Host where the upstream service can be reached.";
      type = types.str;
    };

    port = mkOption {
      description = "Port where the upstream service can be reached.";
      type = types.int;
    };
  };

  urlPartsSubmoduleWithDefaults.options = with lib; let
    inherit (config.services.tsnsrv) defaults;
  in {
    protocol = mkOption {
      description = "URL Scheme or protocol to use for reaching the upstream service.";
      type = types.str;
      default = defaults.urlParts.protocol;
      defaultText = lib.literalExpression "config.services.tsnsrv.defaults.urlParts.protocol";
    };

    host = mkOption {
      description = "Host where the upstream service can be reached.";
      type = types.str;
      default = defaults.urlParts.host;
      defaultText = lib.literalExpression "config.services.tsnsrv.defaults.urlParts.host";
    };

    port = mkOption {
      description = "Port where the upstream service can be reached.";
      type = types.port;
      default = defaults.urlParts.port;
      defaultText = lib.literalExpression "config.services.tsnsrv.defaults.urlParts.port";
    };
  };

  serviceSubmodule = with lib; let
    inherit (config.services.tsnsrv) defaults;
  in ({config, ...}: {
    options = {
      authKeyPath = mkOption {
        description = "Path to a file containing a tailscale auth key. Make this a secret";
        type = types.path;
        default = defaults.authKeyPath;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.authKeyPath";
      };

      ephemeral = mkOption {
        description = "Delete the tailnet participant shortly after it goes offline";
        type = types.bool;
        default = defaults.ephemeral;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.ephemeral";
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
        default = defaults.listenAddr;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.listenAddr";
      };

      loginServerUrl = lib.mkOption {
        description = "Login server URL to use. If unset, defaults to the official tailscale service.";
        default = defaults.loginServerUrl;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.loginServerUrl";
        type = with types; nullOr str;
      };

      package = mkOption {
        description = "Package to use for this tsnsrv service.";
        default = defaults.package;
        type = types.package;
      };

      plaintext = mkOption {
        description = "Whether to serve non-TLS-encrypted plaintext HTTP";
        type = types.bool;
        default = false;
      };

      certificateFile = mkOption {
        description = "Custom certificate file to use for TLS listening instead of Tailscale's builtin way";
        type = with types; nullOr path;
        default = defaults.certificateFile;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.certificateFile";
      };

      certificateKey = mkOption {
        description = "Custom key file to use for TLS listening instead of Tailscale's builtin way.";
        type = with types; nullOr path;
        default = defaults.certificateKey;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.certificateKey";
      };

      acmeHost = mkOption {
        description = "Populate certificateFile and certificateKey option from this certificate name from security.acme module.";
        type = with types; nullOr str;
        default = defaults.acmeHost;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.acmeHost";
      };

      upstreamUnixAddr = mkOption {
        description = "Connect only to the given UNIX Domain Socket";
        type = types.nullOr types.path;
        default = null;
      };

      prefixes = mkOption {
        description = "URL path prefixes to allow in forwarding. Acts as an allowlist but if no prefixes are set, all prefixes are allowed.";
        type = types.listOf (types.strMatching "^(/|tailnet:/|funnel:/).*");
        default = [];
        example = ["tailnet:/" "funnel:/.well-known/"];
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

      urlParts = mkOption {
        description = "URL parts that make up an alternative to the toURL option.";
        type = types.nullOr (types.submodule urlPartsSubmoduleWithDefaults);
        default = null;
      };

      toURL = mkOption {
        description = "URL to forward HTTP requests to. Either this or the urlParts option must be set.";
        type = types.str;
        default = "${config.urlParts.protocol}://${config.urlParts.host}:${toString config.urlParts.port}";
        defaultText = lib.literalExpression "\${config.services.tsnsrv.<name>.urlParts.protocol}://\${config.services.tsnsrv.<name>.urlParts.host}:\${toString config.services.tsnsrv.<name>.urlParts.port}";
      };

      supplementalGroups = mkOption {
        description = "List of groups to run the service under (in addition to the 'tsnsrv' group)";
        type = types.listOf types.str;
        default = defaults.supplementalGroups;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.supplementalGroups";
      };

      tags = mkOption {
        description = "Tags for minting an auth key from an OAuth2 client. Must be prefixed with `tag:`";
        type = types.listOf (types.strMatching "^tag:.*");
        default = defaults.tags;
        example = ["tag:foo" "tag:bar"];
      };

      timeout = mkOption {
        description = "Maximum amount of time that authenticating to the tailscale API may take";
        type = with types; nullOr str;
        default = defaults.timeout;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.timeout";
      };

      tsnetVerbose = mkOption {
        description = "Whether to log verbosely from tsnet. Can be useful for seeing first-time authentication URLs.";
        type = types.bool;
        default = defaults.tsnetVerbose;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.tsnetVerbose";
      };

      upstreamAllowInsecureCiphers = mkOption {
        description = "Whether to allow the upstream to have only ciphersuites that don't offer Perfect Forward Secrecy. If a connection attempt to an upstream returns the error `remote error: tls: handshake failure`, try setting this to true.";
        type = types.bool;
        default = defaults.upstreamAllowInsecureCiphers;
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.upstreamAllowInsecureCiphers";
      };

      extraArgs = mkOption {
        description = "Extra arguments to pass to this tsnsrv process.";
        type = types.listOf types.str;
        default = [];
      };
    };
  });

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
  in
    [
      "-name=${name}"
      "-ephemeral=${lib.boolToString service.ephemeral}"
      "-funnel=${lib.boolToString service.funnel}"
      "-plaintext=${lib.boolToString service.plaintext}"
      "-listenAddr=${service.listenAddr}"
      "-stripPrefix=${lib.boolToString service.stripPrefix}"
      "-insecureHTTPS=${lib.boolToString service.insecureHTTPS}"
      "-suppressTailnetDialer=${lib.boolToString service.suppressTailnetDialer}"
      "-readHeaderTimeout=${readHeaderTimeout}"
      "-tsnetVerbose=${lib.boolToString service.tsnetVerbose}"
      "-upstreamAllowInsecureCiphers=${lib.boolToString service.upstreamAllowInsecureCiphers}"
    ]
    ++ lib.optionals (service.whoisTimeout != null) ["-whoisTimeout" service.whoisTimeout]
    ++ lib.optionals (service.upstreamUnixAddr != null) ["-upstreamUnixAddr" service.upstreamUnixAddr]
    ++ lib.optionals (service.certificateFile != null && service.certificateKey != null) [
      "-certificateFile=${service.certificateFile}"
      "-keyFile=${service.certificateKey}"
    ]
    ++ lib.optionals (service.timeout != null) ["-timeout=${service.timeout}"]
    ++ map (t: "-tag=${t}") service.tags
    ++ map (p: "-prefix=${p}") service.prefixes
    ++ map (h: "-upstreamHeader=${h}") (lib.mapAttrsToList (name: service: "${name}: ${service}") service.upstreamHeaders)
    ++ service.extraArgs
    ++ [service.toURL];
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

      acmeHost = mkOption {
        description = "Populate certificateFile and certificateKey option from this certifcate name from security.acme module.";
        type = with types; nullOr str;
        default = null;
      };

      certificateFile = mkOption {
        description = "Custom certificate file to use for TLS listening instead of Tailscale's builtin way";
        type = with types; nullOr path;
        default = null;
      };

      certificateKey = mkOption {
        description = "Custom key file to use for TLS listening instead of Tailscale's builtin way.";
        type = with types; nullOr path;
        default = null;
      };

      ephemeral = mkOption {
        description = "Delete the tailnet participant shortly after it goes offline";
        type = types.bool;
        default = false;
      };

      listenAddr = mkOption {
        description = "Address to listen on";
        type = types.str;
        default = ":443";
      };

      loginServerUrl = lib.mkOption {
        description = "Login server URL to use. If unset, defaults to the official tailscale service.";
        default = null;
        type = with types; nullOr str;
      };

      supplementalGroups = mkOption {
        description = "List of groups to run the service under (in addition to the 'tsnsrv' group)";
        type = types.listOf types.str;
        default = [];
      };

      tags = mkOption {
        description = "Tags for minting an auth key from an OAuth2 client. Must be prefixed with `tag:`";
        type = types.listOf (types.strMatching "^tag:.*");
        default = [];
        example = ["tag:foo" "tag:bar"];
      };

      timeout = mkOption {
        description = "Maximum amount of time that authenticating to the tailscale API may take";
        type = with types; nullOr str;
        default = null;
      };

      tsnetVerbose = mkOption {
        description = "Whether to log verbosely from tsnet. Can be useful for seeing first-time authentication URLs.";
        type = types.bool;
        default = false;
      };

      upstreamAllowInsecureCiphers = mkOption {
        description = "Whether to require the upstream to support Perfect Forward Secrecy cipher suites. If a connection attempt to an upstream returns the error `remote error: tls: handshake failure`, try setting this to true.";
        type = types.bool;
        default = false;
      };

      urlParts = mkOption {
        description = "Default URL parts for tsnsrv services. Each service will have the parts here interpolated onto its .toURL option by default.";
        type = types.submodule urlPartsSubmodule;
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
        defaultText = lib.literalExpression "config.services.tsnsrv.defaults.authKeyPath";
      };

      containers = mkOption {
        description = "Attrset mapping sidecar container names to their respective tsnsrv service definition. Each sidecar container will be attached to the container it belongs to, sharing its network.";
        type = types.attrsOf (types.submodule {
          options = {
            name = mkOption {
              description = "Name to use for the tsnet service. This defaults to the container name.";
              type = types.nullOr types.str;
              default = null;
            };

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
      (lib.mkIf (config.services.tsnsrv.enable || config.virtualisation.oci-sidecars.tsnsrv.enable)
        {users.groups.tsnsrv = {};})
      (lib.mkIf config.services.tsnsrv.enable {
        assertions =
          lib.mapAttrsToList (name: service: {
            assertion = ((service.certificateFile != null) && (service.certificateKey != null)) || ((service.certificateFile == null) && (service.certificateKey == null));
            message = "Both certificateFile and certificateKey must either be set or null on services.tsnsrv.services.${name}";
          })
          config.services.tsnsrv.services;

        systemd.services =
          lib.mapAttrs' (
            name: service':
              lib.nameValuePair
              "tsnsrv-${name}"
              (let
                service =
                  service'
                  // lib.optionalAttrs (service'.acmeHost != null) {
                    certificateFile = "${config.security.acme.certs.${service.acmeHost}.directory}/fullchain.pem";
                    certificateKey = "${config.security.acme.certs.${service.acmeHost}.directory}/key.pem";
                  };
              in {
                wantedBy = ["multi-user.target"];
                after = ["network-online.target"];
                wants = ["network-online.target"];
                script = ''
                  exec ${lib.getExe service.package} \
                    "-stateDir=$STATE_DIRECTORY/tsnet-tsnsrv" \
                    "-authkeyPath=$CREDENTIALS_DIRECTORY/authKey" \
                    ${lib.escapeShellArgs (serviceArgs {inherit name service;})}
                '';
                stopIfChanged = false;
                serviceConfig =
                  {
                    DynamicUser = true;
                    Restart = "always";
                    SupplementaryGroups = [config.users.groups.tsnsrv.name] ++ service.supplementalGroups;
                    StateDirectory = "tsnsrv-${name}";
                    StateDirectoryMode = "0700";
                    LoadCredential = [
                      "authKey:${service.authKeyPath}"
                    ];
                  }
                  // lib.optionalAttrs (service.loginServerUrl != null) {
                    Environment = "TS_URL=${service.loginServerUrl}";
                  }
                  // lockedDownserviceConfig;
              })
          )
          config.services.tsnsrv.services;
      })

      (lib.mkIf config.virtualisation.oci-sidecars.tsnsrv.enable {
        virtualisation.oci-containers.containers =
          lib.mapAttrs' (name: sidecar: {
            inherit name;
            value = let
              serviceName = "${config.virtualisation.oci-containers.backend}-${name}";
              credentialsDir = "/run/credentials/${serviceName}.service";
            in {
              imageFile = flake.packages.${pkgs.stdenv.targetPlatform.system}.tsnsrvOciImage;
              image = "tsnsrv:latest";
              dependsOn = [sidecar.forContainer];
              user = config.virtualisation.oci-containers.containers.${sidecar.forContainer}.user;
              volumes = [
                # The service's state dir; we have to infer /var/lib
                # because the backends don't support using the
                # $STATE_DIRECTORY environment variable in volume specs.
                "/var/lib/${serviceName}:/state"

                # Same for the service's credentials dir:
                "${credentialsDir}:${credentialsDir}"
              ];
              extraOptions = [
                "--network=container:${sidecar.forContainer}"
              ];
              environment = lib.optionalAttrs (sidecar.service.loginServerUrl != null) {
                TS_URL = sidecar.service.loginServerUrl;
              };
              cmd =
                ["-stateDir=/state" "-authkeyPath=${credentialsDir}/authKey"]
                ++ (serviceArgs {
                  name =
                    if sidecar.name == null
                    then name
                    else sidecar.name;
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
            in {
              name = serviceName;
              value = {
                path = ["/run/wrappers"];
                serviceConfig = {
                  StateDirectory = serviceName;
                  StateDirectoryMode = "0700";
                  SupplementaryGroups = [config.users.groups.tsnsrv.name] ++ sidecar.service.supplementalGroups;
                  LoadCredential = [
                    "authKey:${sidecar.service.authKeyPath}"
                  ];
                };
              };
            })
            config.virtualisation.oci-sidecars.tsnsrv.containers
          )
          // (
            # systemd unit of the container we're sidecar-ing to:
            # Ensure that the sidecar is up when the "main" container is up.
            lib.foldAttrs (item: acc: {unitConfig.Upholds = acc.unitConfig.Upholds ++ ["${item}.service"];})
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
