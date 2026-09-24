package mobile

import "openflux/transport/yandex"

func init() {
	yandex.SetVolgaStartupSink(appendVolgaStartupEvent)
}

// Only enum-derived constants can reach ReadLogs through this channel. Neither
// provider errors nor arbitrary strings are accepted, and debug stays disabled.
func appendVolgaStartupEvent(event yandex.VolgaStartupEvent) {
	name := event.String()
	if name == "" {
		return
	}
	line := "[VOLGA-AUTH] event=" + name
	if class := event.FailureClass(); class != "" {
		line += " class=" + class
	}
	appendLog(line)
}
