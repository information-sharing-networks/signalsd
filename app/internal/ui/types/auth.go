package types

import "github.com/information-sharing-networks/signalsd/app/internal/apperrors"

// These types are shared to avoid circular imports between auth ↔ client ↔ handlers

// AccessTokenDetails represents the response from the signalsd login and refresh token APIs
type AccessTokenDetails struct {

	// AccessToken is the JWT access token
	AccessToken string `json:"access_token"`

	// TokenType is the token type (Bearer)
	TokenType string `json:"token_type"`

	// ExpiresIn is the token expiry in seconds
	ExpiresIn int `json:"expires_in"`

	// AccountID is the account id of the user making the request
	AccountID string `json:"account_id"`

	// AccountType is the account type of the user making the request (user or service_account)
	AccountType string `json:"account_type"`

	// Role is the role of the user making the request (siteadmin, isnadmin, member)
	Role string `json:"role"`

	// IsnPerms is a map of the ISNs the account has access to and the permissions granted (the map key is the isn_slug)
	IsnPerms map[string]IsnPerm `json:"isn_perms,omitempty"`
}

type SignalType struct {

	// Path is the signal type path in the format "slug/v{version}"
	Path string `json:"path"`

	// Slug is the signal type slug
	Slug string `json:"slug"`

	// SemVer is the signal type version (e.g. 0.0.1)
	SemVer string `json:"sem_ver"`

	// InUse is true if the signal type is active for the ISN
	InUse bool `json:"in_use"`
}

type IsnPerm struct {

	// CanRead is true if the account has read access to the isn
	CanRead bool `json:"can_read"`

	// CanWrite is true if the account has write access to the isn
	CanWrite bool `json:"can_write"`

	// CanAdminister is true if the account is the owner of the isn or site admin
	CanAdminister bool `json:"can_administer"`

	// SignalTypes is a map of the signal type paths to the signal type details
	SignalTypes map[string]SignalType `json:"signal_types,omitempty"`

	// Visibility is the ISN visibility setting (public or private)
	Visibility string `json:"visibility"`

	// SignalBatchID is the ID of the current signal batch for the ISN (used for tracking signals when writing to the isn)
	SignalBatchID *string `json:"signal_batch_id,omitempty"`

	// InUse is true if the isn is active
	InUse bool `json:"in_use"`
}

// ErrorResponse represents an error response from the API
// Shared to avoid circular imports between auth ↔ client
type ErrorResponse struct {
	ErrorCode apperrors.ErrorCode `json:"error_code"`
	Message   string              `json:"message"`
}
