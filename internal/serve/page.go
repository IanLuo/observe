package serve

import _ "embed"

// page is the embedded UI. It renders the /api/state contract and computes no
// status of its own (.agents/specs/architecture.md D5). The lock marker for the
// visual direction is .agents/specs/design-system.md.
//
// The page is one file with no framework and no remote asset, which keeps cgo-free
// asset handling and a single embedded request. Slice 5b replaces this composition
// with the TypeScript/SVG map from architecture D4; the wire contract stays.
//
//go:embed static/index.html
var page string
