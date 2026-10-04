package main

// Usage doc: a real markdown file, embedded at build time (see docs/dev-playbook.md §5.0.0).

import _ "embed"

//go:embed docs/bluesky.md
var usageDoc string
