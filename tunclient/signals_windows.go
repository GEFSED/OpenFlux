//go:build windows

package tunclient

import (
	"os"
	"os/signal"
)

func NotifySignals(ch chan os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
