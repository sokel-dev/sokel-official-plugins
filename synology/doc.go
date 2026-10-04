package main

// Usage doc: a real markdown file, embedded at build time (see docs/dev-playbook.md
// §5.0.0). Reported to the platform during the registration handshake — it's what shows
// as "usage doc" in the UI: how to get the credential, what the gotchas are. It travels
// with the plugin code, so a redeploy is all it takes to update.

import _ "embed"

//go:embed docs/synology.md
var usageDoc string
