// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// stringListToTF converts a Go string slice to a Terraform list value.
func stringListToTF(ctx context.Context, in []string) (types.List, diag.Diagnostics) {
	if in == nil {
		in = []string{}
	}
	return types.ListValueFrom(ctx, types.StringType, in)
}

// tfListToStrings converts a Terraform list value to a Go string slice.
func tfListToStrings(ctx context.Context, l types.List) ([]string, diag.Diagnostics) {
	var out []string
	if l.IsNull() || l.IsUnknown() {
		return out, nil
	}
	diags := l.ElementsAs(ctx, &out, false)
	return out, diags
}

// timeToRFC3339 renders a timestamp as an RFC3339 string, or "" when zero.
func timeToRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// timePtrToTF renders an optional timestamp as an RFC3339 string value.
func timePtrToTF(t *time.Time) types.String {
	if t == nil || t.IsZero() {
		return types.StringNull()
	}
	return types.StringValue(t.Format(time.RFC3339))
}
