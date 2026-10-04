package main

// The usage doc is a real markdown file, embedded at compile time (see docs/dev-playbook.md
// §5.0.0). It's reported to the platform during the registration handshake — what shows up under
// "usage notes" in the UI is exactly this file: how to set up the app, which scopes to check, what
// each operation costs. It travels with the plugin code, so redeploying updates it.

import _ "embed"

//go:embed docs/x.md
var usageDoc string
