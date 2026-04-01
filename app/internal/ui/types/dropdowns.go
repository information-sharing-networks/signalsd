package types

// These types are used for UI dropdown components in templates and handlers

type IsnOption struct {
	Slug string `json:"slug"`
}

type SignalTypeSlug struct {
	Slug string `json:"slug"`
}

type SignalTypeVersion struct {
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
