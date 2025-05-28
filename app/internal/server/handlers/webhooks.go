package handlers

import (
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/utils"
)

type WebhookHandler struct {
	queries *database.Queries
}

func NewWebhookHandler(queries *database.Queries) *WebhookHandler {
	return &WebhookHandler{queries: queries}
}

// HandlerWebhooks godocs
//
//	@Summary		Register webhook
//	@Tags			Signals Management
//
//	@Description	TODO - register a webhook to recieve signals batch status updates
//
//	@Router			/webhooks [post]
func (wh *WebhookHandler) HandlerWebhooks(w http.ResponseWriter, r *http.Request) {
	utils.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - webhooks not yet implemented")
}
