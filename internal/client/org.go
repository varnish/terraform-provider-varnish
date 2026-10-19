// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// InviteToOrg invites email to orgID with role "user" or "admin". Sending it
// again with another role updates the pending invite.
func (c *Client) InviteToOrg(ctx context.Context, orgID int, email, role string) error {
	q := url.Values{}
	q.Set("email", email)
	q.Set("role", role)
	return c.do(ctx, http.MethodPost, orgInvitePath(strconv.Itoa(orgID))+"?"+q.Encode(), nil, nil)
}

// ListOrgInvites returns the pending invites for orgID.
func (c *Client) ListOrgInvites(ctx context.Context, orgID int) ([]OrgInvite, error) {
	var out []OrgInvite
	if err := c.do(ctx, http.MethodGet, orgInvitePath(strconv.Itoa(orgID)), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UninviteFromOrg revokes a pending invite for email in orgID.
func (c *Client) UninviteFromOrg(ctx context.Context, orgID int, email string) error {
	q := url.Values{}
	q.Set("email", email)
	return c.do(ctx, http.MethodDelete, orgInvitePath(strconv.Itoa(orgID))+"?"+q.Encode(), nil, nil)
}

// CurrentOrgID returns the org the access token is scoped to.
func (c *Client) CurrentOrgID(ctx context.Context) (int, error) {
	var who WhoAmI
	if err := c.do(ctx, http.MethodGet, whoamiPath(), nil, &who); err != nil {
		return 0, err
	}
	if who.Org.ID == 0 {
		return 0, fmt.Errorf("whoami returned no current organization for this token")
	}
	return who.Org.ID, nil
}

// GetOrg reads an org, including its accepted members in Accounts.
func (c *Client) GetOrg(ctx context.Context, orgID int) (*Org, error) {
	var out Org
	if err := c.do(ctx, http.MethodGet, orgPath(strconv.Itoa(orgID)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UnassignAccount removes an accepted member from orgID. The API reads only the
// account id from the body.
func (c *Client) UnassignAccount(ctx context.Context, orgID, accountID int) error {
	return c.do(ctx, http.MethodPut, orgUnassignPath(strconv.Itoa(orgID)), Account{ID: accountID}, nil)
}

// ListOrgRoles returns the roles available in orgID.
func (c *Client) ListOrgRoles(ctx context.Context, orgID int) ([]Role, error) {
	var out []Role
	if err := c.do(ctx, http.MethodGet, orgRolesPath(strconv.Itoa(orgID)), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAccountRole changes a member's role. The API requires a name, so acct
// must carry the member's current name and email. An empty password is left
// unchanged.
func (c *Client) UpdateAccountRole(ctx context.Context, accountID, roleID int, acct Account) error {
	acct.ID = accountID
	acct.Password = ""
	acct.Role = Role{ID: roleID}
	return c.do(ctx, http.MethodPut, accountPath(strconv.Itoa(accountID)), acct, nil)
}
