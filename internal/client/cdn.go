// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// CreateService creates a service from a wizard payload.
func (c *Client) CreateService(ctx context.Context, in ServiceWizard) (*Service, error) {
	var svc Service
	if err := c.do(ctx, http.MethodPost, servicesPath(), in, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

// GetService reads a service with its domains and origins.
func (c *Client) GetService(ctx context.Context, id int) (*Service, error) {
	var svc Service
	if err := c.do(ctx, http.MethodGet, servicePath(strconv.Itoa(id)), nil, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

// ListServices returns all services for the token's organization.
func (c *Client) ListServices(ctx context.Context) ([]Service, error) {
	var svcs []Service
	if err := c.do(ctx, http.MethodGet, servicesPath(), nil, &svcs); err != nil {
		return nil, err
	}
	return svcs, nil
}

// DeleteService removes a service.
func (c *Client) DeleteService(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodDelete, servicePath(strconv.Itoa(id)), nil, nil)
}

// SetServiceName updates a service's name.
func (c *Client) SetServiceName(ctx context.Context, id int, name string) error {
	return c.do(ctx, http.MethodPut, serviceNamePath(strconv.Itoa(id)), Service{Name: name}, nil)
}

// SetOrigins replaces the full set of origins on a service.
func (c *Client) SetOrigins(ctx context.Context, id int, origins []ServiceOrigin) error {
	return c.do(ctx, http.MethodPut, serviceOriginsPath(strconv.Itoa(id)), origins, nil)
}

// RegenerateOriginSecret generates a new origin secret for a service.
func (c *Client) RegenerateOriginSecret(ctx context.Context, id int) (*Service, error) {
	var svc Service
	if err := c.do(ctx, http.MethodPut, serviceOriginSecretPath(strconv.Itoa(id)), nil, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}

// Deploy deploys a service.
func (c *Client) Deploy(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodPut, serviceDeployPath(strconv.Itoa(id)), nil, nil)
}

// Undeploy undeploys a service.
func (c *Client) Undeploy(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodPut, serviceUndeployPath(strconv.Itoa(id)), nil, nil)
}

// VerifyDNS returns the DNS verification settings for each domain on a service.
func (c *Client) VerifyDNS(ctx context.Context, id int) ([]DNSSettings, error) {
	var out []DNSSettings
	if err := c.do(ctx, http.MethodGet, serviceVerifyDNSPath(strconv.Itoa(id)), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddDomains adds domains to a service and returns its updated domain list.
func (c *Client) AddDomains(ctx context.Context, serviceID int, domains []Domain) ([]Domain, error) {
	var out []Domain
	if err := c.do(ctx, http.MethodPost, serviceDomainsPath(strconv.Itoa(serviceID)), domains, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateDomain updates a single domain on a service.
func (c *Client) UpdateDomain(ctx context.Context, serviceID int, d Domain) (*Domain, error) {
	var out Domain
	if err := c.do(ctx, http.MethodPut, serviceDomainPath(strconv.Itoa(serviceID), strconv.Itoa(d.ID)), d, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDomain removes a domain from a service.
func (c *Client) DeleteDomain(ctx context.Context, serviceID, domainID int) error {
	return c.do(ctx, http.MethodDelete, serviceDomainPath(strconv.Itoa(serviceID), strconv.Itoa(domainID)), nil, nil)
}

// CreateCert uploads a new certificate.
func (c *Client) CreateCert(ctx context.Context, in Cert) (*Cert, error) {
	var out Cert
	if err := c.do(ctx, http.MethodPost, certsPath(), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCert reads a single certificate.
func (c *Client) GetCert(ctx context.Context, id int) (*Cert, error) {
	var out Cert
	if err := c.do(ctx, http.MethodGet, certPath(strconv.Itoa(id)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCerts returns all certificates for the token's organization.
func (c *Client) ListCerts(ctx context.Context) ([]Cert, error) {
	var out []Cert
	if err := c.do(ctx, http.MethodGet, certsPath(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateCert updates an existing certificate.
func (c *Client) UpdateCert(ctx context.Context, in Cert) (*Cert, error) {
	var out Cert
	if err := c.do(ctx, http.MethodPut, certPath(strconv.Itoa(in.ID)), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCert removes a certificate.
func (c *Client) DeleteCert(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodDelete, certPath(strconv.Itoa(id)), nil, nil)
}

// CDNWizardSettings is the normalized wizard document of a service. Settings is
// flat, keyed by wizard string id, and holds every field of the wizard version.
type CDNWizardSettings struct {
	WizardVersion int                 `json:"wizardVersion"`
	Settings      map[string]any      `json:"settings"`
	Overrides     []CDNWizardOverride `json:"overrides"`
}

// CDNWizardOverride is one custom rule: a set of settings applied to matching
// paths, extensions or origin.
type CDNWizardOverride struct {
	Name       string          `json:"name"`
	Paths      []string        `json:"paths"`
	Extensions []string        `json:"extensions"`
	OriginName string          `json:"originName,omitempty"`
	Rules      []CDNWizardRule `json:"rules"`
}

// CDNWizardRule is one overridden setting, encoded as in the flat settings map.
type CDNWizardRule struct {
	StringId string          `json:"stringId"`
	Value    json.RawMessage `json:"value"`
}

// GetWizardSettings returns the service's full normalized wizard settings.
func (c *Client) GetWizardSettings(ctx context.Context, serviceID int) (*CDNWizardSettings, error) {
	var out CDNWizardSettings
	if err := c.do(ctx, http.MethodGet, serviceWizardSettingsPath(strconv.Itoa(serviceID)), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutWizardSettings replaces the wizard settings. The document must be complete
// and carry the WizardVersion that was read. The API answers with the service,
// so callers re-read the settings.
func (c *Client) PutWizardSettings(ctx context.Context, serviceID int, in *CDNWizardSettings) error {
	body := *in
	// the API rejects a missing overrides key, so send [] instead of null
	if body.Overrides == nil {
		body.Overrides = []CDNWizardOverride{}
	}
	return c.do(ctx, http.MethodPut, serviceWizardSettingsPath(strconv.Itoa(serviceID)), body, nil)
}

// ListWizardTemplates returns the available wizard templates.
func (c *Client) ListWizardTemplates(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, http.MethodGet, wizardsPath(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// wizardListEntry is the minimal shape needed to resolve answer template ids.
type wizardListEntry struct {
	ID              int `json:"id"`
	AnswerTemplates []struct {
		ID int `json:"id"`
	} `json:"answerTemplates"`
}

// LatestAnswerTemplateID returns the highest answer template id, used when none
// is configured.
func (c *Client) LatestAnswerTemplateID(ctx context.Context) (int, error) {
	var wizards []wizardListEntry
	if err := c.do(ctx, http.MethodGet, wizardsPath(), nil, &wizards); err != nil {
		return 0, err
	}
	best := 0
	for _, w := range wizards {
		for _, t := range w.AnswerTemplates {
			if t.ID > best {
				best = t.ID
			}
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("no wizard answer templates are available to the organization")
	}
	return best, nil
}
