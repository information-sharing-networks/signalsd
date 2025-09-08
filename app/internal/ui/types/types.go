package types

import (
	"encoding/json"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

// =============================================================================
// AUTHENTICATION & AUTHORIZATION TYPES
// =============================================================================

// AccesTokenDetails represents the response from the signalsd login and refresh token APIs
type AccesTokenDetails struct {
	AccessToken string             `json:"access_token"`
	TokenType   string             `json:"token_type"`
	ExpiresIn   int                `json:"expires_in"`
	AccountID   string             `json:"account_id"`
	AccountType string             `json:"account_type"`
	Role        string             `json:"role"`
	Perms       map[string]IsnPerm `json:"isn_perms,omitempty"`
}

// AccountInfo represents the user's account information stored in cookies
type AccountInfo struct {
	AccountID   string `json:"account_id"`
	AccountType string `json:"account_type"`
	Role        string `json:"role"`
}

// IsnPerm represents permissions for an ISN
type IsnPerm struct {
	Permission      string   `json:"permission"`
	SignalBatchID   string   `json:"signal_batch_id"`
	SignalTypePaths []string `json:"signal_types"`
	Visibility      string   `json:"visibility"` // "public" or "private"
	IsnAdmin        bool     `json:"isn_admin"`  // true if the account is the owner of the isn or the site owner
}

// =============================================================================
// API RESPONSE TYPES
// =============================================================================

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	ErrorCode apperrors.ErrorCode `json:"error_code"`
	Message   string              `json:"message"`
}

// =============================================================================
// DROPDOWN OPTIONS
// =============================================================================

type IsnDropdown struct {
	Slug       string `json:"slug"`
	IsInUse    bool   `json:"is_in_use"`
	Visibility string `json:"visibility"`
}

type SignalTypeDropdown struct {
	Slug string `json:"slug"`
}

type VersionDropdown struct {
	Version string `json:"version"`
}

// =============================================================================
// SIGNAL SEARCH TYPES
// =============================================================================

// SignalSearchParams represents search parameters for signals
type SignalSearchParams struct {
	IsnSlug                 string
	SignalTypeSlug          string
	SemVer                  string
	StartDate               string
	EndDate                 string
	AccountID               string
	SignalID                string
	LocalRef                string
	IncludeWithdrawn        bool
	IncludeCorrelated       bool
	IncludePreviousVersions bool
}

// SearchSignal represents a signal in search results
type SearchSignal struct {
	AccountID            string          `json:"account_id"`
	AccountType          string          `json:"account_type"`
	Email                string          `json:"email,omitempty"`
	SignalID             string          `json:"signal_id"`
	LocalRef             string          `json:"local_ref"`
	SignalCreatedAt      string          `json:"signal_created_at"`
	SignalVersionID      string          `json:"signal_version_id"`
	VersionNumber        int32           `json:"version_number"`
	VersionCreatedAt     string          `json:"version_created_at"`
	CorrelatedToSignalID string          `json:"correlated_to_signal_id"`
	IsWithdrawn          bool            `json:"is_withdrawn"`
	Content              json.RawMessage `json:"content"`
}

// PreviousSignalVersion represents a previous version of a signal
type PreviousSignalVersion struct {
	SignalVersionID string          `json:"signal_version_id"`
	CreatedAt       string          `json:"created_at"`
	VersionNumber   int32           `json:"version_number"`
	Content         json.RawMessage `json:"content"`
}

// SearchSignalWithCorrelationsAndVersions represents a signal with optional correlations and versions
type SearchSignalWithCorrelationsAndVersions struct {
	SearchSignal
	CorrelatedSignals      []SearchSignal          `json:"correlated_signals,omitempty"`
	PreviousSignalVersions []PreviousSignalVersion `json:"previous_signal_versions,omitempty"`
}

// SignalSearchResponse represents the response from signal search (direct array)
type SignalSearchResponse []SearchSignalWithCorrelationsAndVersions
