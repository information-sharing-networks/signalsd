package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

type HandlerService struct {
	AuthService *auth.AuthService
	ApiClient   *client.Client
	Environment string
}

// getIsnOptions is a helper that returns a list of ISNs for the dropdown list. The list is used as a parater on several pages,
// Optionally the selected value can be use to trigger a cascading update of the signal type dropdown list (see SignalTypeOptionsHandler)
//
// If filterByIsnAdmin is true, only ISNs where the user is an admin are returned.
// If filterByWritePerm is true, only ISNs where the user has write permission are returned.
func (h *HandlerService) getIsnOptions(isnPerms map[string]types.IsnPerm, filterByIsnAdmin bool, filterByWritePerm bool) []types.IsnOption {
	isns := make([]types.IsnOption, 0, len(isnPerms))
	for isnSlug, perm := range isnPerms {
		if filterByIsnAdmin && !perm.IsnAdmin {
			continue
		}
		if filterByWritePerm && perm.Permission != "write" {
			continue
		}
		isns = append(isns, types.IsnOption{
			Slug:    isnSlug,
			IsInUse: true,
		})
	}
	return isns
}

// RenderSignalTypeOptions gets the signal types for the selected ISN and renders the dropdown options
func (h *HandlerService) RenderSignalTypeOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	isnSlug := r.FormValue("isn-slug")
	if isnSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get permissions data from context
	perms, ok := auth.ContextIsnPerms(r.Context())
	if !ok {
		reqLogger.Error("Failed to get ISN permissions from context in signal types handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get signal types for the selected ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Parse signal type paths to extract unique signal types
	signalTypeMap := make(map[string]bool)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 {
			signalTypeMap[parts[0]] = true
		}
	}

	// Convert to slice of SignalTypeOption
	signalTypes := make([]types.SignalTypeOption, 0, len(signalTypeMap))
	for signalType := range signalTypeMap {
		signalTypes = append(signalTypes, types.SignalTypeOption{
			Slug: signalType,
		})
	}

	// Render signal types dropdown options
	component := templates.SignalTypeOptions(signalTypes)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type options", slog.String("error", err.Error()))
	}
}

// RenderSignalTypeVersionOptions gets the versions for the selected signal type and returns the dropdown options
func (h *HandlerService) RenderSignalTypeVersionOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	isnSlug := r.FormValue("isn-slug")
	signalTypeSlug := r.FormValue("signal-type-slug")
	if isnSlug == "" || signalTypeSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get permissions data from context
	perms, ok := auth.ContextIsnPerms(r.Context())
	if !ok {
		reqLogger.Error("Failed to get ISN permissions from context in versions handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get signal types for the selected ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Find versions for the specific signal type
	versions := make([]types.VersionOption, 0)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 && parts[0] == signalTypeSlug {
			versions = append(versions, types.VersionOption{
				Version: parts[1],
			})
		}
	}

	// Render version dropdown options
	component := templates.SignalTypeVersionOptions(versions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render version options", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) RenderUserOptionsGeneratePasswordLink(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get access token from context
	accessToken, ok := auth.ContextAccessToken(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}
	// Get users from API
	users, err := h.ApiClient.GetUserOptionsList(accessToken)
	if err != nil {
		reqLogger.Error("Failed to get users", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load users. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Render user dropdown options
	component := templates.UserOptionsGeneratePasswordLink(users)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render web user options", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) RenderServiceAccountOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get access token from context
	accessToken, ok := auth.ContextAccessToken(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get service accounts from API
	ServiceAccountOptions, err := h.ApiClient.GetServiceAccountOptionsList(accessToken)
	if err != nil {
		reqLogger.Error("Failed to get service accounts", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load service accounts. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Render service account dropdown with reissue button integration
	component := templates.ServiceAccountSelectorForReissue(ServiceAccountOptions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render service account options", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) RenderAccountIdentifierField(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accountType := r.FormValue("account-type")
	if accountType == "" {
		// Return placeholder if no account type selected
		component := templates.AccountIdentifierPlaceholder()
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render account identifier placeholder", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessToken, ok := auth.ContextAccessToken(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	switch accountType {
	case "user":
		users, err := h.ApiClient.GetUserOptionsList(accessToken)
		if err != nil {
			reqLogger.Error("Failed to get users", slog.String("error", err.Error()))
			component := templates.ErrorAlert("Failed to load users. Please try again.")
			if err := component.Render(r.Context(), w); err != nil {
				reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
			}
			return
		}

		component := templates.UserSelectorForAccountIdentifier(users)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render user dropdown", slog.String("error", err.Error()))
		}

	case "service-account":
		serviceAccounts, err := h.ApiClient.GetServiceAccountOptionsList(accessToken)
		if err != nil {
			reqLogger.Error("Failed to get service accounts", slog.String("error", err.Error()))
			component := templates.ErrorAlert("Failed to load service accounts. Please try again.")
			if err := component.Render(r.Context(), w); err != nil {
				reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
			}
			return
		}

		// Render service account dropdown
		component := templates.ServiceAccountSelectorForAccountIdentifier(serviceAccounts)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render service account dropdown", slog.String("error", err.Error()))
		}

	default:
		component := templates.AccountIdentifierPlaceholder()
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render account identifier placeholder", slog.String("error", err.Error()))
		}
	}
}
