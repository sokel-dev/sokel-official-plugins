package main

// Usage docs: an actual markdown file, embedded at compile time (see docs/dev-playbook.md
// §5.0.0). It's reported to the platform during the registration handshake, and the "usage
// docs" shown in the UI is exactly this — how to get a credential, what the pitfalls are — it
// travels with the plugin code and updates on the next deploy.

import _ "embed"

//go:embed docs/gmail.md
var usageDoc string
