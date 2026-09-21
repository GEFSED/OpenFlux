package yandex

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	d "universal-bypass-tool/internal/receivediag"
)

func TestVolgaPayloadBoundary(t *testing.T) {
	packet := []byte{1, 2, 3, 4}
	frame := make([]byte, 2+len(packet))
	binary.BigEndian.PutUint16(frame, uint16(len(packet)))
	copy(frame[2:], packet)
	raw, _ := json.Marshal(base64.StdEncoding.EncodeToString(frame))
	var got []byte
	w := &wsListener{stats: &VolgaStats{}, onData: func(p []byte) { got = append([]byte(nil), p...) }}
	before := d.Default.Snapshot()
	w.handleBundleItem(raw)
	after := d.Default.Snapshot()
	for i, n := range after.Values {
		want := uint64(0)
		if d.Metric(i) == d.VolgaFrames {
			want = 1
		}
		if d.Metric(i) == d.VolgaBytes {
			want = uint64(len(packet))
		}
		if n-before.Values[i] != want {
			t.Errorf("unexpected %s delta", d.Names[i])
		}
	}
	if !bytes.Equal(got, packet) {
		t.Fatal("Volga callback bytes changed")
	}
}
