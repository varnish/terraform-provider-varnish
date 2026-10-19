// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/net/publicsuffix"
)

const wildcardPrefix = "*."

// same pattern as the API
var subdomainRegex = regexp.MustCompile(`(?i)^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))*$`)

// isApex reports whether fqdn is a registrable domain such as example.com or example.co.uk.
func isApex(fqdn string) bool {
	apex, err := publicsuffix.EffectiveTLDPlusOne(fqdn)
	return err == nil && apex == fqdn
}

// cdnCheckDomain applies the API's fqdn/subdomain rules. An apex without a
// subdomain is only a warning, since accounts without apex redirects accept it.
func cdnCheckDomain(fqdn, subdomain string) (warning, problem string) {
	fqdn = strings.ToLower(fqdn)

	switch {
	case strings.HasPrefix(fqdn, wildcardPrefix):
		if subdomain != "" {
			return "", fmt.Sprintf("%s is a wildcard domain, which cannot have a subdomain.", fqdn)
		}
	case isApex(fqdn):
		if subdomain == "" {
			return fmt.Sprintf("%[1]s is an apex domain, so the API requires a subdomain, e.g. \"www\". "+
				"The service is then served on www.%[1]s, and %[1]s redirects to it. "+
				"Only accounts without apex redirect support accept an apex without one.", fqdn), ""
		}
		if !subdomainRegex.MatchString(subdomain) {
			return "", fmt.Sprintf("%q is not a valid subdomain.", subdomain)
		}
	default:
		if subdomain != "" {
			return "", fmt.Sprintf("subdomain is only used for apex domains, and %[1]s is not one. "+
				"Remove subdomain, or set fqdn = \"%[2]s.%[1]s\".", fqdn, subdomain)
		}
	}
	return "", ""
}

// validateDomains runs cdnCheckDomain on every domain block whose values are known.
func (r *cdnServiceResource) validateDomains(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var list types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("domain"), &list)...)
	if resp.Diagnostics.HasError() || list.IsNull() || list.IsUnknown() {
		return
	}

	for i, e := range list.Elements() {
		obj, ok := e.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}

		fqdn, fqdnOK := knownString(obj.Attributes()["fqdn"])
		sub, subOK := knownString(obj.Attributes()["subdomain"])
		if !fqdnOK || !subOK {
			continue
		}

		at := path.Root("domain").AtListIndex(i).AtName("subdomain")
		warning, problem := cdnCheckDomain(fqdn, sub)
		if warning != "" {
			resp.Diagnostics.AddAttributeWarning(at, "Apex domain without subdomain", warning)
		}
		if problem != "" {
			resp.Diagnostics.AddAttributeError(at, "Invalid subdomain", problem)
		}
	}
}

// knownString returns the value of a string attribute. A null value counts as "".
func knownString(v attr.Value) (string, bool) {
	s, ok := v.(types.String)
	if !ok || s.IsUnknown() {
		return "", false
	}
	return s.ValueString(), true
}
