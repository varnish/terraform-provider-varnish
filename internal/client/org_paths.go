// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

// These org-level URLs pass cdnBase explicitly because the gateway serves every
// route, org routes included, under /api/v1/cdn today.

func whoamiPath() string               { return apiPath(cdnBase, authBase, "whoami") }
func orgPath(id string) string         { return apiPath(cdnBase, orgBase, id) }
func orgInvitePath(id string) string   { return apiPath(cdnBase, orgBase, id, "invite") }
func orgUnassignPath(id string) string { return apiPath(cdnBase, orgBase, id, "unassign") }
func orgRolesPath(id string) string    { return apiPath(cdnBase, orgBase, id, roleBase) }
func accountPath(id string) string     { return apiPath(cdnBase, accountBase, id) }
