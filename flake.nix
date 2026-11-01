{
  description = "Observe — live project progress against locked goals";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # macOS first (.agents/specs/observe-progress-view.md, Platform).
      systems = [ "aarch64-darwin" "x86_64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAll (pkgs: {
        default = pkgs.buildGoModule {
          pname = "observe";
          version = "0.0.0";
          src = self;
          # proxyVendor keeps each module whole: `go mod vendor` drops the
          # tree-sitter C sources and headers that cgo needs.
          proxyVendor = true;
          vendorHash = "sha256-o67HzDbvJqsESiKoAFrP2g2+iosm9eGY5EmN1SvHXzc=";
          subPackages = [ "cmd/observe" ];
          # The tests build fixture repositories, so they need git.
          nativeBuildInputs = [ pkgs.git ];
        };
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.git ];
        };
      });
    };
}
