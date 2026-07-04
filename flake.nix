{
  description = "issues — lightweight issue tracker (HTTP CRUD service + CLI over markdown)";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        issues = pkgs.callPackage ./nix/package.nix { };
        default = issues;
      });

      # Importable on any host. Set services.issues.package to
      # inputs.issues.packages.${system}.default for a pinned build.
      nixosModules.default = import ./nix/module.nix;
      nixosModules.issues = import ./nix/module.nix;

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell { packages = [ pkgs.go ]; };
      });
    };
}
