package types

// =============================================================================
// DROPDOWN OPTIONS
// =============================================================================
// These types are used for UI dropdown components in templates and handlers

type IsnOption struct {
	Slug          string `json:"slug"`
	IsInUse       bool   `json:"is_in_use"`
	Visibility    string `json:"visibility"`
	UserAccountID string `json:"user_account_id"`
}

type SignalTypeSlugOption struct {
	Slug string `json:"slug"`
}

type VersionOption struct {
	Version string `json:"version"`
}

type ServiceAccountOption struct {
	ClientOrganization string `json:"client_organization"`
	ClientContactEmail string `json:"client_contact_email"`
	ClientID           string `json:"client_id"`
}

type UserOption struct {
	Email    string `json:"email"`
	UserRole string `json:"user_role"`
}
