{
  description = "adept — author AI skills, agents, and loops once, render them into every coding harness";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # release-please owns the version; the flake just reads it.
      version = (builtins.fromJSON (builtins.readFile ./.release-please-manifest.json)).".";
    in
    {
      packages = forAllSystems (pkgs: rec {
        adeptability = pkgs.buildGoModule {
          pname = "adeptability";
          inherit version;
          src = self;

          vendorHash = "sha256-W+7efGpI+4d9db4EHFHcYoOnGwwEIGy8N+7YMbbsOdE=";

          subPackages = [ "cmd/adept" ];

          nativeBuildInputs = [ pkgs.installShellFiles ];

          # Completions run the freshly built binary, so skip them when the
          # build platform can't execute host binaries (cross builds).
          postInstall = pkgs.lib.optionalString (pkgs.stdenv.buildPlatform.canExecute pkgs.stdenv.hostPlatform) ''
            installShellCompletion --cmd adept \
              --bash <($out/bin/adept completion bash) \
              --zsh <($out/bin/adept completion zsh) \
              --fish <($out/bin/adept completion fish)
          '';

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
            "-X main.commit=${self.rev or self.dirtyRev or "unknown"}"
            "-X main.date=${self.lastModifiedDate or "unknown"}"
          ];

          # CI runs the full gate matrix (vet, lint, race, e2e) on every PR;
          # the flake only needs to produce the binary. The e2e suite also
          # rebuilds the binary with `go build`, which the sandbox disallows.
          doCheck = false;

          meta = {
            description = "Cross-harness AI skill, agent, and loop portability CLI";
            homepage = "https://github.com/itaywol/adeptability";
            license = nixpkgs.lib.licenses.mit;
            mainProgram = "adept";
          };
        };
        default = adeptability;
      });

      overlays.default = final: _prev: {
        adeptability = self.packages.${final.stdenv.hostPlatform.system}.adeptability;
      };

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            golangci-lint
          ];
        };
      });
    };
}
