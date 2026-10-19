// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// CreatePurgeToken creates a purge token and returns its JWT pair. The response
// has no id, see FindNewPurgeToken.
func (c *Client) CreatePurgeToken(ctx context.Context, in NewPurgeToken) (*TokenSet, error) {
	var out TokenSet
	if err := c.do(ctx, http.MethodPost, purgeTokensPath(), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPurgeToken reads a single purge token.
func (c *Client) GetPurgeToken(ctx context.Context, id int) (*PurgeToken, error) {
	var out PurgeToken
	if err := c.do(ctx, http.MethodGet, purgeTokenPath(strconv.Itoa(id)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPurgeTokens returns all purge tokens of the token's organization.
func (c *Client) ListPurgeTokens(ctx context.Context) ([]PurgeToken, error) {
	var out []PurgeToken
	if err := c.do(ctx, http.MethodGet, purgeTokensPath(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeletePurgeToken removes a purge token, revoking it.
func (c *Client) DeletePurgeToken(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodDelete, purgeTokenPath(strconv.Itoa(id)), nil, nil)
}

// PurgeTokenIDs returns the stored ids as a set. Take it before a create so the
// new token can be found.
func (c *Client) PurgeTokenIDs(ctx context.Context) (map[int]bool, error) {
	tokens, err := c.ListPurgeTokens(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[int]bool, len(tokens))
	for _, t := range tokens {
		ids[t.ID] = true
	}
	return ids, nil
}

// FindNewPurgeToken returns the token with label that is not in before. Labels
// are not unique. Two concurrent creates with the same label return an error.
func (c *Client) FindNewPurgeToken(ctx context.Context, before map[int]bool, label string) (*PurgeToken, error) {
	tokens, err := c.ListPurgeTokens(ctx)
	if err != nil {
		return nil, err
	}

	var found []PurgeToken
	for _, t := range tokens {
		if !before[t.ID] && t.Label == label {
			found = append(found, t)
		}
	}

	switch len(found) {
	case 1:
		return &found[0], nil
	case 0:
		return nil, fmt.Errorf("purge token %q was created but is not in the token list", label)
	default:
		ids := make([]int, 0, len(found))
		for _, t := range found {
			ids = append(ids, t.ID)
		}
		return nil, fmt.Errorf("%d new purge tokens labelled %q appeared (ids %v); cannot tell which one was just created", len(found), label, ids)
	}
}
