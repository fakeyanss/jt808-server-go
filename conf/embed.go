// Package conf holds the default server configuration data in this directory and
// embeds it into the binary, so the server can run standalone without an
// external conf directory.
//
// go:embed 只能引用本包目录内的文件，因此嵌入声明必须与配置数据同目录；
// 除 internal/config 外没有其它包依赖本包。
package conf

import "embed"

//go:embed *.yaml *.txt
var FS embed.FS
