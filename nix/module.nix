# NixOS module: services.issues — host-agnostic. Enable on any host with a
# single `services.issues.enable = true`. Host-specific values (port, dataDir,
# bind address, token) are exposed as options.
{ config, lib, pkgs, ... }:

let
  cfg = config.services.issues;
in
{
  options.services.issues = {
    enable = lib.mkEnableOption "issues tracker (HTTP CRUD service over markdown)";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage ./package.nix { }";
      description = "The issues package to run. Override with the flake's package for pinned builds.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8787;
      description = "TCP port the HTTP API listens on.";
    };

    bindAddress = lib.mkOption {
      type = lib.types.str;
      default = "0.0.0.0";
      example = "192.0.2.10";
      description = "Address to bind. Use a private or VPN interface address to limit reachability.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/issues";
      description = ''
        Directory holding the markdown issue files. With the default value the service
        provisions it via systemd StateDirectory. If you change it, make sure the
        path exists and is writable by the service user.
      '';
    };

    tokenFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/run/secrets/issues-token";
      description = "Optional file with a static bearer token. Null = no auth; rely on network isolation.";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open the listen port in the firewall.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.issues = {
      description = "issues tracker (HTTP CRUD over markdown)";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];

      serviceConfig = {
        ExecStart = lib.concatStringsSep " " ([
          "${lib.getExe cfg.package}"
          "serve"
          "-data ${cfg.dataDir}"
          "-addr ${cfg.bindAddress}"
          "-port ${toString cfg.port}"
        ] ++ lib.optional (cfg.tokenFile != null) "-token-file ${cfg.tokenFile}");

        # Run as a dynamic, unprivileged user. StateDirectory provisions
        # /var/lib/issues (the default dataDir) owned by that user.
        DynamicUser = true;
        StateDirectory = lib.mkIf (cfg.dataDir == "/var/lib/issues") "issues";

        Restart = "on-failure";
        RestartSec = 2;

        # hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ReadWritePaths = [ cfg.dataDir ];
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" ];
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
      };
    };

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];
  };
}
