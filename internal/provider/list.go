// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"iter"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
)

// listResults yields one result per item, filled by fill, up to the request limit.
// fill returns false to skip an item, e.g. one deleted since the list call.
func listResults[T any](ctx context.Context, req list.ListRequest, items []T, fill func(T, *list.ListResult) bool) iter.Seq[list.ListResult] {
	return func(push func(list.ListResult) bool) {
		var n int64
		for _, item := range items {
			if req.Limit > 0 && n >= req.Limit {
				return
			}
			result := req.NewListResult(ctx)
			if !fill(item, &result) {
				continue
			}
			n++
			if !push(result) {
				return
			}
		}
	}
}

// listError ends the stream with a single error.
func listError(stream *list.ListResultsStream, summary, detail string) {
	var diags diag.Diagnostics
	diags.AddError(summary, detail)
	stream.Results = list.ListResultsStreamDiagnostics(diags)
}
