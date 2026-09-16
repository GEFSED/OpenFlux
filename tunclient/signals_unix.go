//go:build !windows

package tunclient

import (
	"os"
	"os/signal"
	"syscall"
)

func NotifySignals(ch chan os.Signal) {
	// SIGHUP is what a closed terminal window sends the foreground process
	// (not SIGINT/SIGTERM) - without catching it, closing the window instead
	// of Ctrl+C skips the route cleanup in runClientTUN entirely.
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
}
