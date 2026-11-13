//go:build unix

package media

import (
	"syscall"
	"time"
)

// readProcessCPU 读取本进程累计 CPU 时间（user + sys）。
// 取不到时返回 false，调用方据此少报 cpuPercent，心跳不失败。
func readProcessCPU() (time.Duration, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, false
	}
	return time.Duration(usage.Utime.Sec)*time.Second + time.Duration(usage.Utime.Usec)*time.Microsecond +
		time.Duration(usage.Stime.Sec)*time.Second + time.Duration(usage.Stime.Usec)*time.Microsecond, true
}
