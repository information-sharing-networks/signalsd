package types

// =============================================================================
// DROPDOWN OPTIONS
// =============================================================================
// These types are used for UI dropdown components in templates and handlers

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
