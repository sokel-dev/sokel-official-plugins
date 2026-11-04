# Microsoft OneDrive plugin

Browse, search, upload small files and explicitly track drive changes. See the [user guide](docs/microsoft-onedrive.md) for scopes, supported operations and limitations.

The manifest is authoritative and generates `zz_*.go` with public Sokel Plugin SDK v0.7.7. There are no private dependencies, local module replacements, OAuth provider declarations or automatic token refresh. API origin is fixed to global Graph v1.0; tests inject an HTTP transport into a loopback fixture instead of adding a production base-URL override.

Run from this module:

```sh
go generate ./...
go test -race -cover ./...
go vet ./...
go build ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

The tests began with compilable failing handler stubs. They assert actual HTTP method/path/query/header/body bytes, generated input/output binding, explicit zero/false/null values, page boundaries, response limits, redirect rejection, safe errors and no write retries. Fixtures are synthetic cases based on the official references in the user guide, not captured live responses. Review API evolution against those sources before changing projections. No real Microsoft tenant acceptance, registry publication or image publication is claimed.

Default interface and documentation text is English. `locales/zh-CN.json` translates every display string and the entire exact user guide. Regenerate and validate after changing either the manifest or guide. Keep this module independent; do not edit generated files directly.
