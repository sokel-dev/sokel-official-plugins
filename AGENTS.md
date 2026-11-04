# Official plugin development

Use English as the default language for new and changed plugin interfaces: plugin names,
operation and field labels, descriptions, credential help, runtime errors, README files,
and the default user guide. Keep Chinese translations in `locales/zh-CN.json`; translation
keys must match the English source exactly. Localized guides may use `*.zh-CN.md`.
This repository requirement overrides the older Chinese-default convention in shared
plugin development guides. Do not rewrite unrelated existing plugins solely to migrate
their language.

New integrations must declare `plugin.icon` and ship a recognizable product icon as a local
SVG or PNG. Keep its source, modification notes and applicable license in `<icon>.license`.
Validate the asset with the pinned SDK and verify it renders at plugin-card size. Do not
depend on platform name inference or a remote image URL for the plugin's identity.

Declare contracts before implementation and regenerate them with the SDK version pinned
by the module. Never hand-edit `zz_*.go`. New integrations must build independently
against the public SDK, with no local `replace` directives or private dependencies.

Validate request/response behavior, credential handling, pagination, and event checkpoints.
Run generation checks, `go test -race ./...`, `go vet ./...`, and `go build ./...` for changed
Go plugins. Distinguish fixture tests from live service acceptance. Never invent registry
digests or claim publication before artifacts have been built and verified.
