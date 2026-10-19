// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"net/http"

	"golang.org/x/time/rate"
)

// rateLimitTransport enforces a client-side rate limit. The token is set in
// doOnce, not here, so net/http drops it on cross-host redirects.
type rateLimitTransport struct {
	limiter *rate.Limiter
	next    http.RoundTripper
}

func newRateLimitTransport(limiter *rate.Limiter, next http.RoundTripper) *rateLimitTransport {
	if next == nil {
		next = http.DefaultTransport
	}
	return &rateLimitTransport{limiter: limiter, next: next}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.limiter != nil {
		if err := t.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	return t.next.RoundTrip(req)
}
