package main

// Usage doc: a real markdown file, embedded at compile time.

import _ "embed"

//go:embed docs/producthunt.md
var usageDoc string
