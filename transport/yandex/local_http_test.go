package yandex

import "net/http"

type localRoundTripper func(*http.Request) (*http.Response, error)

func (f localRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
