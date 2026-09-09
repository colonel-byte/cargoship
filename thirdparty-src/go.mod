// This file intentionally makes thirdparty-src its own Go module boundary. The .go files under
// it are raw upstream source pulled by `mage generate:pullEngineSource` for static go/ast
// parsing only -- they are never meant to compile or be imported, so the main module's
// build/vet/lint tooling must not descend into them. See docs/dev/thirdparty-src.md.
module thirdpartysrc

go 1.21
