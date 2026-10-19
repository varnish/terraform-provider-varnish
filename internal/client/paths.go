// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"path"
)

// URL segments of the REST API.
const (
	apiBase     = "/api"
	apiVersion  = "v1"
	accountBase = "accounts"
	authBase    = "auth"
	domainBase  = "domains"
	orgBase     = "orgs"
	roleBase    = "roles"
	serviceBase = "services"
	sessionBase = "sessions"
	wizardBase  = "wizards"
)

// apiPath joins the API base and version with segments. Callers pass their
// product base as the first segment.
func apiPath(segments ...string) string {
	return path.Join(append([]string{apiBase, apiVersion}, segments...)...)
}
