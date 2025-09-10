package types

import "github.com/information-sharing-networks/signalsd/app/internal/apperrors"

// =============================================================================
// AUTHENTICATION & AUTHORIZATION TYPES
// =============================================================================
// These types are shared to avoid circular imports between auth ↔ client ↔ handlers

// AccessTokenDetails represents the response from the signalsd login and refresh token APIs
type AccessTokenDetails struct {
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

// ErrorResponse represents an error response from the API
// Shared to avoid circular imports between auth ↔ client
type ErrorResponse struct {
	ErrorCode apperrors.ErrorCode `json:"error_code"`
	Message   string              `json:"message"`
}
