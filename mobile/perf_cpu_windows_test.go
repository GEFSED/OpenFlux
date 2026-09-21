//go:build windows

package mobile

import "syscall"

func perfProcessCPUSeconds() float64 {
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(syscall.Handle(^uintptr(0)), &created, &exited, &kernel, &user); err != nil {
		return -1
	}
	ticks := func(t syscall.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	return float64(ticks(kernel)+ticks(user)) / 1e7
}
