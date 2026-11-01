// Package configs embeds the default server configuration files into the binary,
// so the server can run standalone without an external configs directory.
package configs

import "embed"

//go:embed *.yaml *.txt
var FS embed.FS
