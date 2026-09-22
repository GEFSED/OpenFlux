package yandex

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
)

// Numeric-only relay outcome counters. Timeout and network are disjoint;
// 429 is a subset of 4xx. No error text or response data is retained.
type volgaHTTPFailureCounters struct {
	network     atomic.Uint64
	timeout     atomic.Uint64
	clientError atomic.Uint64
	rateLimited atomic.Uint64
	serverError atomic.Uint64
	otherStatus atomic.Uint64
}

type VolgaHTTPFailureDiagnostics struct {
	Total       uint64 `json:"http_failures_total"`
	Network     uint64 `json:"http_failures_network"`
	Timeout     uint64 `json:"http_failures_timeout"`
	ClientError uint64 `json:"http_failures_4xx"`
	RateLimited uint64 `json:"http_failures_429"`
	ServerError uint64 `json:"http_failures_5xx"`
	OtherStatus uint64 `json:"http_failures_other_status"`
}

func (c *volgaHTTPFailureCounters) recordNetwork(err error) {
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		c.timeout.Add(1)
	} else {
		c.network.Add(1)
	}
}

func (c *volgaHTTPFailureCounters) recordStatus(status int) {
	switch {
	case status >= 400 && status <= 499:
		c.clientError.Add(1)
		if status == 429 {
			c.rateLimited.Add(1)
		}
	case status >= 500 && status <= 599:
		c.serverError.Add(1)
	default:
		c.otherStatus.Add(1)
	}
}

func (c *volgaHTTPFailureCounters) snapshot(total uint64) VolgaHTTPFailureDiagnostics {
	return VolgaHTTPFailureDiagnostics{
		Total:       total,
		Network:     c.network.Load(),
		Timeout:     c.timeout.Load(),
		ClientError: c.clientError.Load(),
		RateLimited: c.rateLimited.Load(),
		ServerError: c.serverError.Load(),
		OtherStatus: c.otherStatus.Load(),
	}
}
