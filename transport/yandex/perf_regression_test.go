package yandex

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"openflux/transport"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestVolgaDefaultsUnchanged(t *testing.T) {
	want := VolgaConfig{2000, 4000, 90 * time.Second, 30 * time.Second, 2000, 1000000, 20, 2 * time.Millisecond, 4 << 20, 5000000, 200, 500 * time.Millisecond, 30 * time.Second, 1.5, 10 * time.Second, 60 * time.Second, 10 * time.Second}
	if DefaultVolgaConfig() != want {
		t.Fatal("CLI defaults changed")
	}
	if NewYandexVolgaTransport("synthetic-context", transport.DefaultConfig()).config != want {
		t.Fatal("legacy constructor changed")
	}
	bad := want
	bad.WorkerCount = 0
	if _, err := NewYandexVolgaTransportWithConfig("", transport.DefaultConfig(), bad); err == nil {
		t.Fatal("invalid config accepted")
	}
	if _, found := reflect.TypeOf(relayClient{}).FieldByName("queue"); found {
		t.Fatal("unused million-entry queue returned")
	}
}
func TestBase64BoundedAndWireIdentical(t *testing.T) {
	for _, n := range []int{0, 1, 200, 1400, 8192, 65535, 4 << 20} {
		p := bytes.Repeat([]byte{173}, n)
		if base64Encode(p) != base64.StdEncoding.EncodeToString(p) {
			t.Fatal("wire output changed")
		}
	}
	if !retainBase64Buffer(256<<10) || retainBase64Buffer((256<<10)+1) || retainBase64Buffer(16<<20) {
		t.Fatal("pool retention is not bounded")
	}
	fresh := b64BufPool.New().([]byte)
	if cap(fresh) != 4096 {
		t.Fatal("excessive cold pool allocation")
	}
}
func TestRelaySendStop100Cycles(t *testing.T) {
	cfg := DefaultVolgaConfig()
	cfg.WorkerCount = 4
	cfg.QueueSize = 16
	jar, _ := cookiejar.New(nil)
	for i := 0; i < 100; i++ {
		r := newRelayClient(&volgaAuth{Session: &http.Client{Jar: jar}}, cfg, &VolgaStats{})
		r.httpClient.Transport = labRoundTripper(func(req *http.Request) (*http.Response, error) {
			req.Body.Close()
			return &http.Response{StatusCode: 204, Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
		})
		r.Start()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.Send([]byte("synthetic"))
			}
		}()
		r.Stop()
		wg.Wait()
		if len(r.batchQueue) != 0 || cap(r.batchQueue) != 16 {
			t.Fatal("stopped relay retained data")
		}
		if r.Send([]byte("late")) == nil {
			t.Fatal("stopped send accepted")
		}
	}
}
