{
  description = "Observe — live project progress against locked goals";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # macOS first (.agents/specs/observe-progress-view.md, Platform).
      systems = [ "aarch64-darwin" "x86_64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # The UI bundle. web/ is the source; this derivation is the release build, so
      # the binary never depends on the committed app.js being up to date.
      webFor = pkgs: pkgs.stdenv.mkDerivation {
        pname = "observe-web";
        version = "0.0.0";
        src = ./web;
        nativeBuildInputs = [ pkgs.esbuild pkgs.typescript ];
        buildPhase = ''
          runHook preBuild
          cp tsconfig.json . 2>/dev/null || true
          tsc --noEmit -p tsconfig.json
          esbuild main.ts --bundle --minify --target=es2022 --format=iife \
            --outfile=app.js \
            --banner:js='/* generated from web/ by web/build.sh — edit web/, not this file */'
          runHook postBuild
        '';
        installPhase = ''
          runHook preInstall
          mkdir -p $out
          cp app.js $out/app.js
          runHook postInstall
        '';
      };
    in
    {
      packages = forAll (pkgs:
        let web = webFor pkgs;
        in {
          default = pkgs.buildGoModule {
            pname = "observe";
            version = "0.0.0";
            src = self;
            # proxyVendor keeps each module whole: `go mod vendor` drops the
            # tree-sitter C sources and headers that cgo needs.
            proxyVendor = true;
            vendorHash = "sha256-0oc+ZBVlVUGNMB+Paei41Y+6La1+kfhYHlPSIbKZJe8=";
            subPackages = [ "cmd/observe" ];
            # The tests build fixture repositories, so they need git.
            nativeBuildInputs = [ pkgs.git ];
            # The bundle is embedded by internal/serve, so it must exist before Go
            # compiles. Copying it here keeps the release build independent of the
            # checked-in copy.
            preBuild = ''
              cp ${web}/app.js internal/serve/static/app.js
            '';
          };
          inherit web;
        });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.git pkgs.nodejs_22 pkgs.esbuild pkgs.typescript ];
        };
      });
    };
}
