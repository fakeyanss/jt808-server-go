//go:build !unix

package media

import "time"

// readProcessCPU 在缺少进程 CPU 采集能力的平台上直接降级：不报该项。
func readProcessCPU() (time.Duration, bool) {
	return 0, false
}
