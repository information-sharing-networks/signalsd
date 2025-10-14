package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// renderAccessDeniedPage is a helper function that renders access denied pages with different messages
func (h *HandlerService) renderAccessDeniedPage(w http.ResponseWriter, r *http.Request, message string) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.AccessDeniedPage(h.Environment, "Access Denied", message)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render access denied page", slog.String("error", err.Error()))
	}
}

// AccessDeniedPage renders the general access denied page.
func (h *HandlerService) AccessDeniedPage(w http.ResponseWriter, r *http.Request) {
	h.renderAccessDeniedPage(w, r, "You do not have permission to use this feature")
}

// AccessDeniedNeedIsnAdminPage renders the access denied page for users who need to create ISNs.
func (h *HandlerService) AccessDeniedNeedIsnAdminPage(w http.ResponseWriter, r *http.Request) {
	h.renderAccessDeniedPage(w, r, "You need to create one or more ISNs before accessing this page")
}

// AccessDeniedNeedIsnAccessPage renders the access denied page for users who need to be added to ISNs.
func (h *HandlerService) AccessDeniedNeedIsnAccessPage(w http.ResponseWriter, r *http.Request) {
	h.renderAccessDeniedPage(w, r, "You need to be added to one or more ISNs before accessing this page")
}
