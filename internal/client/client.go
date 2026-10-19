// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

// Package client is a small typed HTTP client for the Varnish REST API. Request and response types are in types.go.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// DefaultUserAgent identifies the provider in API request logs.
const DefaultUserAgent = "terraform-provider-varnish"

// DefaultEndpoint is the public CDN API gateway, used when none is configured.
const DefaultEndpoint = "https://api.varnish-cdn.com"

// Client talks to the CDN API using a long-lived bearer access token.
type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
	userAgent  string
}

// Config configures a Client.
type Config struct {
	// Endpoint is the base URL of the API gateway, e.g. https://api.varnish-cdn.com
	Endpoint string
	// AccessToken is the long-lived CDN API access token.
	AccessToken string
	// Insecure disables TLS certificate verification (local/test stacks only).
	Insecure bool
	// UserAgent overrides the default User-Agent header.
	UserAgent string
}

// New builds a Client from cfg.
func New(cfg Config) *Client {
	// clone the default transport to keep its proxy, dial and TLS timeouts
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for test stacks
	}

	limiter := rate.NewLimiter(rate.Limit(5), 10)
	ua := cfg.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}

	return &Client{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		token:    cfg.AccessToken,
		httpClient: &http.Client{
			Timeout:   60 * time.Second,
			Transport: newRateLimitTransport(limiter, transport),
			// the API never redirects, so treat a redirect as the response
			// rather than replaying the request (and its token) elsewhere
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		userAgent: ua,
	}
}

const (
	// maxResponseBytes bounds what a response can make the provider allocate.
	maxResponseBytes = 8 << 20
	// maxErrorBodyBytes bounds how much of a failed response is kept in the error.
	maxErrorBodyBytes = 4096
)

// errorBody trims a response body down to what an error message can carry.
func errorBody(b []byte) string {
	if len(b) <= maxErrorBodyBytes {
		return string(b)
	}
	return string(b[:maxErrorBodyBytes]) + "... (truncated)"
}

// deployLockRetryInterval and deployLockTimeout bound retries while a service
// deployment triggered by a prior operation is still running.
const (
	deployLockRetryInterval = 3 * time.Second
	deployLockTimeout       = 2 * time.Minute
)

// isDeployLock reports whether err is the "deployment is currently running"
// error, which clears once the running deploy finishes.
func isDeployLock(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && strings.Contains(apiErr.Body, "a deployment is currently running")
}

// do sends body as JSON and decodes the response into out, when each is non-nil.
// Calls blocked by a running deployment are retried until deployLockTimeout.
func (c *Client) do(ctx context.Context, method, p string, body, out any) error {
	deadline := time.Now().Add(deployLockTimeout)
	for {
		err := c.doOnce(ctx, method, p, body, out)
		if err == nil || !isDeployLock(err) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deployLockRetryInterval):
		}
	}
}

func (c *Client) doOnce(ctx context.Context, method, p string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+p, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, p, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Method: method, Path: p, Body: errorBody(respBody)}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// EndpointURL returns the configured base endpoint (used for diagnostics).
func (c *Client) EndpointURL() string { return c.endpoint }
