package main

// Usage doc: an actual markdown file, embedded at compile time (see docs/dev-playbook.md §5.0.0).
// It's reported to the platform along with the registration handshake, and is exactly what shows
// up as "usage instructions" in the UI.

import _ "embed"

//go:embed docs/tushare.md
var usageDoc string
