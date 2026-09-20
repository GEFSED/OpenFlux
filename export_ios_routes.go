//go:build ios

package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"openflux/internal/transportstack"
)

// OpenFluxResolveBypassIPv4 must be called BEFORE setTunnelNetworkSettings.
// Returns a JSON array or nil. Uses the same Yandex-first DoT resolver as Go.
// Release the result with OpenFluxFreeString. Never log its inputs.
//
//export OpenFluxResolveBypassIPv4
func OpenFluxResolveBypassIPv4(transportType, url *C.char) *C.char {
	hosts, err := transportstack.BypassHosts(C.GoString(transportType), C.GoString(url))
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ips, err := transportstack.ResolveBypassIPv4(ctx, hosts, net.DefaultResolver.LookupIPAddr)
	if err != nil {
		return nil
	}
	data, err := json.Marshal(ips)
	if err != nil {
		return nil
	}
	return C.CString(string(data))
}
