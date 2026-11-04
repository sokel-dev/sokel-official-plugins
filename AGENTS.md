# Official plugin development

Use English as the default language for new and changed plugin interfaces: plugin names,
operation and field labels, descriptions, credential help, runtime errors, README files,
and the default user guide. Keep Chinese translations in `locales/zh-CN.json`; translation
keys must match the English source exactly. Localized guides may use `*.zh-CN.md`.
This repository requirement overrides the older Chinese-default convention in shared
plugin development guides. Do not rewrite unrelated existing plugins solely to migrate
their language.

Declare contracts before implementation and regenerate them with the SDK version pinned
by the module. Never hand-edit `zz_*.go`. New integrations must build independently
against the public SDK, with no local `replace` directives or private dependencies.

Validate request/response behavior, credential handling, pagination, and event checkpoints.
Run generation checks, `go test -race ./...`, `go vet ./...`, and `go build ./...` for changed
Go plugins. Distinguish fixture tests from live service acceptance. Never invent registry
digests or claim publication before artifacts have been built and verified.
