package transportstack

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sort"
)

// BypassHosts lists control/relay endpoints used by the actual constructors.
// Resolve before installing a default TUN route. No deployment IPs live here.
func BypassHosts(kind, documentURL string) ([]string, error) {
	u, err := url.Parse(documentURL)
	if err != nil {
		return nil, errors.New("invalid document URL")
	}
	var hosts []string
	switch kind {
	case "yandex", "vyandex":
		if u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, errors.New("invalid document URL")
		}
		hosts = []string{u.Hostname(), "disk.yandex.ru", "docs.yandex.ru", "docviewer.yandex.ru"}
		if kind == "vyandex" {
			hosts = append(hosts, "volga.yandex.ru", "push.yandex.ru")
		}
	case "oneme":
		hosts = []string{"ws-api.oneme.ru", "web.max.ru"}
	default:
		return nil, errors.New("unsupported transport")
	}
	return hosts, nil
}

type IPLookup func(context.Context, string) ([]net.IPAddr, error)

// ResolveBypassIPv4 returns only validated IPv4 literals, safe for /32 routes.
// Failed required lookups abort setup instead of silently risking a routing loop.
func ResolveBypassIPv4(ctx context.Context, hosts []string, lookup IPLookup) ([]string, error) {
	unique := map[string]bool{"77.88.8.8": true, "8.8.8.8": true, "1.1.1.1": true}
	for _, host := range hosts {
		ips, err := lookup(ctx, host)
		if err != nil {
			return nil, errors.New("cannot resolve transport bypass endpoints")
		}
		for _, ip := range ips {
			if v4 := ip.IP.To4(); v4 != nil {
				unique[v4.String()] = true
			}
		}
	}
	ips := make([]string, 0, len(unique))
	for ip := range unique {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips, nil
}
