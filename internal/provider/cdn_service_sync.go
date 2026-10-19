// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/varnish/terraform-provider-varnish/internal/client"
)

// cdnDefaultOriginIndex returns the index of the origin marked is_default, or 0.
func cdnDefaultOriginIndex(origins []client.ServiceOrigin) int {
	for i, o := range origins {
		if o.IsDefault {
			return i
		}
	}
	return 0
}

func cdnOriginToAPI(m cdnOriginModel) client.ServiceOrigin {
	return client.ServiceOrigin{
		Name:             m.Name.ValueString(),
		Host:             m.Host.ValueString(),
		Port:             int(m.Port.ValueInt64()),
		EnableTLS:        m.EnableTLS.ValueBool(),
		HostHeader:       m.HostHeader.ValueString(),
		IsDefault:        m.IsDefault.ValueBool(),
		AllowInsecureTLS: m.AllowInsecureTLS.ValueBool(),
	}
}

func cdnOriginsToAPI(ms []cdnOriginModel) []client.ServiceOrigin {
	out := make([]client.ServiceOrigin, 0, len(ms))
	for _, m := range ms {
		out = append(out, cdnOriginToAPI(m))
	}
	return out
}

func cdnOriginToModel(o client.ServiceOrigin) cdnOriginModel {
	return cdnOriginModel{
		Name:             types.StringValue(o.Name),
		Host:             types.StringValue(o.Host),
		Port:             types.Int64Value(int64(o.Port)),
		EnableTLS:        types.BoolValue(o.EnableTLS),
		HostHeader:       types.StringValue(o.HostHeader),
		IsDefault:        types.BoolValue(o.IsDefault),
		AllowInsecureTLS: types.BoolValue(o.AllowInsecureTLS),
	}
}

func cdnDomainToAPI(m cdnDomainModel, id int) client.Domain {
	return client.Domain{
		ID:        id,
		FQDN:      m.FQDN.ValueString(),
		Subdomain: m.Subdomain.ValueString(),
		TLS:       m.TLS.ValueBool(),
		CertID:    int(m.CertID.ValueInt64()),
	}
}

// cdnDomainCertID returns the certificate attached to a domain. ACME certificates
// map to null so they do not show as drift against an unset cert_id.
func cdnDomainCertID(d client.Domain) types.Int64 {
	if d.CertType == client.CertLocationACME || d.CertID == 0 {
		return types.Int64Null()
	}
	return types.Int64Value(int64(d.CertID))
}

func cdnDomainsToAPI(ms []cdnDomainModel) []client.Domain {
	out := make([]client.Domain, 0, len(ms))
	for _, m := range ms {
		out = append(out, cdnDomainToAPI(m, 0))
	}
	return out
}

func cdnDomainToModel(d client.Domain) cdnDomainModel {
	return cdnDomainModel{
		FQDN:       types.StringValue(d.FQDN),
		Subdomain:  types.StringValue(d.Subdomain),
		TLS:        types.BoolValue(d.TLS),
		CertID:     cdnDomainCertID(d),
		ID:         types.StringValue(strconv.Itoa(d.ID)),
		Status:     types.StringValue(d.Status.String()),
		TXTRecord:  types.StringValue(d.TXTRecord),
		VerifiedAt: timePtrToTF(d.VerifiedAt),
		IsCDN:      types.BoolValue(d.IsCDN),
	}
}

// cdnOrderedOrigins returns origins in prior order by name, then new ones.
func cdnOrderedOrigins(prior []cdnOriginModel, server []client.ServiceOrigin) []cdnOriginModel {
	byName := make(map[string]client.ServiceOrigin, len(server))
	for _, o := range server {
		byName[o.Name] = o
	}

	out := make([]cdnOriginModel, 0, len(server))
	used := make(map[string]bool, len(server))
	for _, p := range prior {
		name := p.Name.ValueString()
		if o, ok := byName[name]; ok && !used[name] {
			out = append(out, cdnOriginToModel(o))
			used[name] = true
		}
	}
	for _, o := range server {
		if !used[o.Name] {
			out = append(out, cdnOriginToModel(o))
			used[o.Name] = true
		}
	}
	return out
}

// cdnDomainKey identifies a domain by fqdn and subdomain.
func cdnDomainKey(fqdn, subdomain string) string { return fqdn + "\x00" + subdomain }

// cdnOrderedDomains returns domains in prior order by fqdn+subdomain, then new
// ones. The generated CDN domain (IsCDN) is skipped.
func cdnOrderedDomains(prior []cdnDomainModel, server []client.Domain) []cdnDomainModel {
	filtered := make([]client.Domain, 0, len(server))
	for _, d := range server {
		if d.IsCDN {
			continue
		}
		filtered = append(filtered, d)
	}
	server = filtered

	byKey := make(map[string]client.Domain, len(server))
	for _, d := range server {
		byKey[cdnDomainKey(d.FQDN, d.Subdomain)] = d
	}

	out := make([]cdnDomainModel, 0, len(server))
	used := make(map[string]bool, len(server))
	for _, p := range prior {
		k := cdnDomainKey(p.FQDN.ValueString(), p.Subdomain.ValueString())
		if d, ok := byKey[k]; ok && !used[k] {
			out = append(out, cdnDomainToModel(d))
			used[k] = true
		}
	}
	for _, d := range server {
		k := cdnDomainKey(d.FQDN, d.Subdomain)
		if !used[k] {
			out = append(out, cdnDomainToModel(d))
			used[k] = true
		}
	}
	return out
}

// cdnOriginsChanged reports whether the desired origin set differs from state.
func cdnOriginsChanged(state, plan []cdnOriginModel) bool {
	a := cdnOriginsToAPI(state)
	b := cdnOriginsToAPI(plan)
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

type cdnDomainUpdate struct {
	id   int
	plan cdnDomainModel
}

type cdnDomainPlan struct {
	toAdd    []cdnDomainModel
	toUpdate []cdnDomainUpdate
	toDelete []int
}

// cdnDiffDomains diffs plan and state domains by fqdn+subdomain into adds,
// deletes and tls/cert_id updates. A subdomain change is a delete plus an add.
func cdnDiffDomains(state, plan []cdnDomainModel) cdnDomainPlan {
	type stateEntry struct {
		id int
		m  cdnDomainModel
	}
	stateByKey := make(map[string]stateEntry, len(state))
	for _, s := range state {
		id, _ := strconv.Atoi(s.ID.ValueString())
		stateByKey[cdnDomainKey(s.FQDN.ValueString(), s.Subdomain.ValueString())] = stateEntry{id: id, m: s}
	}

	var out cdnDomainPlan
	planKeys := make(map[string]bool, len(plan))
	for _, p := range plan {
		k := cdnDomainKey(p.FQDN.ValueString(), p.Subdomain.ValueString())
		planKeys[k] = true
		s, ok := stateByKey[k]
		if !ok {
			out.toAdd = append(out.toAdd, p)
			continue
		}
		if cdnDomainFieldsChanged(s.m, p) {
			// never send an unknown cert_id as 0: with TLS on, the API reads that
			// as "detach and issue ACME"
			if p.CertID.IsUnknown() {
				p.CertID = s.m.CertID
			}
			out.toUpdate = append(out.toUpdate, cdnDomainUpdate{id: s.id, plan: p})
		}
	}
	for _, s := range state {
		if !planKeys[cdnDomainKey(s.FQDN.ValueString(), s.Subdomain.ValueString())] {
			id, _ := strconv.Atoi(s.ID.ValueString())
			out.toDelete = append(out.toDelete, id)
		}
	}
	return out
}

// cdnDomainFieldsChanged reports whether mutable domain fields differ. Unknown values count as unchanged.
func cdnDomainFieldsChanged(state, plan cdnDomainModel) bool {
	if !plan.TLS.IsUnknown() && plan.TLS != state.TLS {
		return true
	}
	if !plan.Subdomain.IsUnknown() && plan.Subdomain != state.Subdomain {
		return true
	}
	// cert_id is only managed when set
	if !plan.CertID.IsUnknown() && !plan.CertID.IsNull() && plan.CertID != state.CertID {
		return true
	}
	return false
}
