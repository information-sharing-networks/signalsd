package ui

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// AUTHENTICATION & AUTHORIZATION TYPES
// =============================================================================

// LoginResponse represents the response from the authentication API
type LoginResponse struct {
	AccessToken string              `json:"access_token"`
	TokenType   string              `json:"token_type"`
	ExpiresIn   int                 `json:"expires_in"`
	AccountID   string              `json:"account_id"`
	AccountType string              `json:"account_type"`
	Role        string              `json:"role"`
	Perms       map[string]IsnPerms `json:"isn_perms,omitempty"`
}

// IsnPerms represents permissions for an ISN
type IsnPerms struct {
	Permission      string   `json:"permission"`
	SignalBatchID   string   `json:"signal_batch_id"`
	SignalTypePaths []string `json:"signal_types"`
	Visibility      string   `json:"visibility"` // "public" or "private"
}

// =============================================================================
// API RESPONSE TYPES
// =============================================================================

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

// =============================================================================
// CORE DOMAIN TYPES
// =============================================================================

// ISN represents an Information Sharing Network
type ISN struct {
	ID         uuid.UUID `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Title      string    `json:"title"`
	Slug       string    `json:"slug"`
	Detail     string    `json:"detail"`
	IsInUse    bool      `json:"is_in_use"`
	Visibility string    `json:"visibility"`
}

// SignalType represents a signal type definition
type SignalType struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Slug      string    `json:"slug"`
	SchemaURL string    `json:"schema_url"`
	ReadmeURL string    `json:"readme_url"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	SemVer    string    `json:"sem_ver"`
	IsInUse   bool      `json:"is_in_use"`
}

// =============================================================================
// SIGNAL SEARCH TYPES
// =============================================================================

// SignalSearchParams represents search parameters for signals
type SignalSearchParams struct {
	ISNSlug                 string
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
