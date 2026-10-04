package main

// Usage doc: a real markdown file, embedded at compile time (see docs/dev-playbook.md §5.0.0).
// Reported to the platform during the registration handshake; what the UI shows as "usage doc" is
// exactly this file.

import _ "embed"

//go:embed docs/kbstore-es.md
var usageDoc string
