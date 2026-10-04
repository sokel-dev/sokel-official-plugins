package main

// The usage doc is a real markdown file, embedded at compile time (see docs/dev-playbook.md
// §5.0.0). It's reported to the platform during the registration handshake — what shows up under
// "usage notes" in the UI is exactly this file. It travels with the plugin code, so redeploying
// updates it: how to set up the integration, how to share a page with it, how to wire up real-time
// events.

import _ "embed"

//go:embed docs/notion.md
var usageDoc string
