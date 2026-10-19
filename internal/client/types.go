// Copyright (c) Varnish Software AB
// SPDX-License-Identifier: MPL-2.0

// Wire types for the REST API. Only the fields the provider reads or
// writes are kept.

package client

import "time"

// ServiceStatus is the deployment state of a CDN service.
type ServiceStatus int

const (
	ServiceStatusUnknown    ServiceStatus = 0
	ServiceStatusDeployed   ServiceStatus = 1
	ServiceStatusUndeployed ServiceStatus = 2
)

func (s ServiceStatus) String() string {
	switch s {
	case ServiceStatusUnknown:
		return "unknown"
	case ServiceStatusDeployed:
		return "deployed"
	case ServiceStatusUndeployed:
		return "undeployed"
	}
	return "undefined"
}

// VerificationStatus is the domain ownership verification state.
type VerificationStatus int

const (
	VerificationStatusUnknown  VerificationStatus = 0
	VerificationStatusPending  VerificationStatus = 1
	VerificationStatusVerified VerificationStatus = 2
)

func (s VerificationStatus) String() string {
	switch s {
	case VerificationStatusUnknown:
		return "unknown"
	case VerificationStatusPending:
		return "pending"
	case VerificationStatusVerified:
		return "verified"
	}
	return "undefined"
}

// CertLocation says where a certificate lives. ACME certificates are issued by the CDN.
type CertLocation int

const (
	CertLocationNotDefined CertLocation = 0
	CertLocationDB         CertLocation = 1
	CertLocationACME       CertLocation = 3
)

func (c CertLocation) String() string {
	switch c {
	case CertLocationDB:
		return "Database"
	case CertLocationACME:
		return "ACME"
	}
	return "Unknown"
}

// Service is a CDN service with its origins and domains.
type Service struct {
	ID           int             `json:"id"`
	Name         string          `json:"name"`
	Status       ServiceStatus   `json:"status"`
	OriginSecret string          `json:"originSecret"`
	Domains      []Domain        `json:"domains"`
	Origins      []ServiceOrigin `json:"origins"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	// 0 for services created before the API recorded it
	WizardAnswerTemplateID int `json:"wizardAnswerTemplateId,omitempty"`
}

// ServiceOrigin is one backend a service fetches from.
type ServiceOrigin struct {
	Name             string `json:"name"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	EnableTLS        bool   `json:"enableTLS"`
	HostHeader       string `json:"hostHeader"`
	IsDefault        bool   `json:"isDefault"`
	AllowInsecureTLS bool   `json:"allowInsecureTLS"`
}

// ServiceWizard is the create-service payload.
type ServiceWizard struct {
	Origins                []ServiceOrigin `json:"origins"`
	WizardAnswerTemplateID int             `json:"wizardAnswerTemplateId"`
	Domains                []Domain        `json:"domains"`
}

// Domain is one hostname served by a service.
type Domain struct {
	ID         int                `json:"id"`
	FQDN       string             `json:"fqdn"`
	Subdomain  string             `json:"subdomain"`
	TLS        bool               `json:"tls"`
	Status     VerificationStatus `json:"status"`
	TXTRecord  string             `json:"txtRecord"`
	ServiceID  int                `json:"serviceId"`
	VerifiedAt *time.Time         `json:"verifiedAt"`
	IsCDN      bool               `json:"isCDN"` // generated cdn domain
	CertID     int                `json:"certID"`
	CertType   CertLocation       `json:"certType"`
}

// DNSSettings is the live DNS verification result for one domain.
type DNSSettings struct {
	DomainID         int          `json:"domainId"`
	Domain           string       `json:"domain"`
	Subdomain        string       `json:"subdomain"`
	IsApex           bool         `json:"isApex"`
	ValidCNAME       bool         `json:"validCNAME"`
	ValidTXT         bool         `json:"validTXT"`
	ValidA           bool         `json:"validA"`
	CDNRouted        bool         `json:"cdnRouted"`
	Verified         bool         `json:"verified"`
	ARecords         []string     `json:"aRecords"`
	TXTRecords       []string     `json:"txtRecords"`
	CNAME            string       `json:"cname"`
	ExpectedARecords []string     `json:"expectedARecords"`
	ExpectedCNAME    string       `json:"expectedCNAME"`
	ExpectedTXT      string       `json:"expectedTXT"`
	Acme             AcmeSettings `json:"acme"`
}

// AcmeSettings is the ACME part of a domain's DNS verification.
type AcmeSettings struct {
	ValidCNAME    bool   `json:"validCNAME"`
	ExpectedCNAME string `json:"expectedCNAME"`
}

// Cert is an uploaded or ACME-issued TLS certificate.
type Cert struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	PrivateKey   string       `json:"privateKey,omitempty"`
	Certificate  string       `json:"certificate,omitempty"`
	CipherSuites []string     `json:"cipherSuites"`
	MaxVersion   string       `json:"maxVersion"`
	MinVersion   string       `json:"minVersion"`
	NotBefore    *time.Time   `json:"notBefore,omitempty"`
	NotAfter     *time.Time   `json:"notAfter,omitempty"`
	CommonName   string       `json:"commonName"`
	SAN          []string     `json:"san"`
	Type         CertLocation `json:"type"`
}

// NewPurgeToken is the create-purge-token payload. The API uses the field names as is.
type NewPurgeToken struct {
	Label         string `json:"Label"`
	Expire        string `json:"Expire,omitempty"`
	BasicUsername string `json:"BasicUsername,omitempty"`
	BasicSecret   string `json:"BasicSecret,omitempty"`
}

// TokenSet is the JWT pair returned once, when a purge token is created.
type TokenSet struct {
	AccessToken   string `json:"accessToken,omitempty"`
	RefreshToken  string `json:"refreshToken,omitempty"`
	AccessExpire  int64  `json:"accessExpire,omitempty"`
	RefreshExpire int64  `json:"refreshExpire,omitempty"`
}

// PurgeToken is a stored purge token, without its JWTs.
type PurgeToken struct {
	ID     int       `json:"id"`
	OrgID  int       `json:"orgId"`
	Label  string    `json:"label"`
	Expire time.Time `json:"expire"`
}

// Org is an organization and, on read, its accepted members.
type Org struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Accounts []Account `json:"accounts"`
}

// Account is a user account.
type Account struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     Role   `json:"role"`
}

// Role is a named permission set within an organization.
type Role struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	OrgID       int    `json:"orgId"`
	Description string `json:"description"`
}

// OrgInvite is a pending invite.
type OrgInvite struct {
	Email     string    `json:"email"`
	Role      string    `json:"role,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// WhoAmI identifies the organization an access token is scoped to.
type WhoAmI struct {
	Account Account `json:"account"`
	Org     Org     `json:"org"`
}
