package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// NeedIsnAdminPage rednders the access denied page.
func (h *HandlerService) NeedIsnAdminPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.NeedIsnAdminPage("Access Denied", "You need to create one or more ISNs before accessing this page")
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render need ISN admin ", slog.String("error", err.Error()))
	}
}
