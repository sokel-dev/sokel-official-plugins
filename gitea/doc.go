package main

import (
	_ "embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

//go:embed docs/gitea.md
var usageDoc string

func RegisterDoc(h plugin.Host) { plugin.DeclareDoc(h, usageDoc, "") }
