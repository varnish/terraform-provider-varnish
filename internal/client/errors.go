// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is returned for any non-2xx response from the API.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s %s: unexpected status %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// NotFound reports whether the error is a 404 from the API.
func (e *APIError) NotFound() bool { return e.StatusCode == http.StatusNotFound }

// Forbidden reports whether the error is a 403 from the API.
func (e *APIError) Forbidden() bool { return e.StatusCode == http.StatusForbidden }

// IsNotFound reports whether err is an APIError with a 404 status.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.NotFound()
}

// IsForbidden reports whether err is an APIError with a 403 status.
func IsForbidden(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Forbidden()
}
