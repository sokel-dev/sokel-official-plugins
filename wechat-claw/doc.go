package main

// Usage doc: a real markdown file, embedded at compile time (see docs/dev-playbook.md §5.0.0).
// Reported to the platform during the registration handshake; what the UI shows as "usage doc" is
// exactly this file — how to get credentials, what the gotchas are. It travels with the plugin
// code, so a redeploy picks up any update.

import _ "embed"

//go:embed docs/wechat-claw.md
var usageDoc string
