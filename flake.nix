{
  description = "A Nix-flake-based Go 1.26 development environment";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2511";
    nixpkgs-unstable.url = "https://flakehub.com/f/NixOS/nixpkgs/0.1";

    go-tooling = {
      url = "github:NixOS/nixpkgs/8cec5ef1131078ea76689e84c362b0ed6a1fb6a1";
      flake = false;
    };

    goreleaser = {
      url = "github:goreleaser/nur/52282e9a2d7c6882e9e0e8675d593b928c89e323";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, nixpkgs-unstable, goreleaser, go-tooling }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      forEachSupportedSystem = f:
        nixpkgs.lib.genAttrs supportedSystems (system:
          let
            pkgs = import nixpkgs {
              inherit system;
              overlays = [ ];
              config.allowUnfreePredicate = pkg: builtins.elem (nixpkgs.lib.getName pkg) [
                "goreleaser-pro"
              ];
            };
            pkgs-unstable = import nixpkgs-unstable {
              inherit system;
            };
          in
          f { pkgs = pkgs; pkgs-unstable = pkgs-unstable; system = system; }
        );
    in
    {
      devShells = forEachSupportedSystem ({ pkgs, pkgs-unstable, system }:
        let
          go = pkgs.callPackage "${go-tooling}/pkgs/development/compilers/go/1.26.nix" {
            buildGo126Module = buildGoModule;
          };
          buildGoModule = pkgs.buildGoModule.override { inherit go; };
          lintGo = pkgs.callPackage "${go-tooling}/pkgs/development/compilers/go/1.27.nix" {
            buildGo127Module = lintBuildGoModule;
          };
          lintBuildGoModule = pkgs.buildGoModule.override { go = lintGo; };
          golangci-lint = pkgs.callPackage "${go-tooling}/pkgs/by-name/go/golangci-lint/package.nix" {
            buildGo127Module = lintBuildGoModule;
          };
          stablePackages = with pkgs; [
            earthly
            ginkgo
            go
            gotools
            just
            kind
            kubectl
            kubernetes-helm
            kustomize_4
            setup-envtest
            yq
          ];
          toolingPackages = [
            golangci-lint
          ];
          otherPackages = [
            (pkgs.callPackage "${goreleaser}/pkgs/goreleaser-pro" { })
          ];
        in
        {
          default = pkgs.mkShell {
            GOTOOLCHAIN = "local";
            GOROOT = "${go}/share/go";
            packages = stablePackages ++ toolingPackages ++ otherPackages;
          };
        }
      );
    };
}
