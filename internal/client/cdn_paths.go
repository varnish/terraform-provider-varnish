// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

const cdnBase = "cdn"

// cdnPath builds a path under the CDN product base.
func cdnPath(segments ...string) string {
	return apiPath(append([]string{cdnBase}, segments...)...)
}

func servicesPath() string         { return cdnPath(serviceBase) }
func servicePath(id string) string { return cdnPath(serviceBase, id) }

func serviceNamePath(id string) string         { return cdnPath(serviceBase, id, "name") }
func serviceOriginsPath(id string) string      { return cdnPath(serviceBase, id, "origins") }
func serviceOriginSecretPath(id string) string { return cdnPath(serviceBase, id, "originsecret") }
func serviceDeployPath(id string) string       { return cdnPath(serviceBase, id, "deploy") }
func serviceUndeployPath(id string) string     { return cdnPath(serviceBase, id, "undeploy") }
func serviceVerifyDNSPath(id string) string    { return cdnPath(serviceBase, id, "verify-dns") }
func serviceWizardSettingsPath(id string) string {
	return cdnPath(serviceBase, id, "wizard", "settings")
}
func serviceDomainsPath(id string) string { return cdnPath(serviceBase, id, domainBase) }
func serviceDomainPath(sid, did string) string {
	return cdnPath(serviceBase, sid, domainBase, did)
}

func certsPath() string         { return cdnPath("certs") }
func certPath(id string) string { return cdnPath("certs", id) }

func wizardsPath() string { return cdnPath(wizardBase) }

func purgeTokensPath() string         { return cdnPath(sessionBase, "purger") }
func purgeTokenPath(id string) string { return cdnPath(sessionBase, "purger", id) }
