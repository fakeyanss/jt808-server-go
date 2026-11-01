// Package configs embeds the default client simulator configuration into the binary.
package configs

import "embed"

//go:embed *.yaml
var FS embed.FS
