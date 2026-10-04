package main

// Usage doc: a real markdown file, embedded at compile time (docs/dev-playbook.md §5.0.0 in the platform repo).

import _ "embed"

//go:embed docs/hackernews.md
var usageDoc string
