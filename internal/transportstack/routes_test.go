package transportstack

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
)

func TestVolgaRoutesResolveActualHosts(t *testing.T) {
	hosts, err := BypassHosts("vyandex", testURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"volga.yandex.ru", "push.yandex.ru", "document.invalid"} {
		if !slices.Contains(hosts, host) {
			t.Fatalf("missing endpoint %s", host)
		}
	}
	ips, err := ResolveBypassIPv4(context.Background(), hosts, func(_ context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}, {IP: net.ParseIP("2001:db8::1")}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ips, "192.0.2.10") || !slices.Contains(ips, "77.88.8.8") || slices.Contains(ips, "2001:db8::1") {
		t.Fatal("incorrect route families/resolver routes")
	}
}

func TestRouteFailureIsSanitized(t *testing.T) {
	_, err := ResolveBypassIPv4(context.Background(), []string{"document.invalid"}, func(context.Context, string) ([]net.IPAddr, error) { return nil, errors.New("secret failure detail") })
	if err == nil || err.Error() == "secret failure detail" {
		t.Fatal("lookup failure leaked or ignored")
	}
}
