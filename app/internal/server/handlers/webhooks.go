package handlers

import (
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
)

type WebhookHandler struct {
	queries *database.Queries
}

func NewWebhookHandler(queries *database.Queries) *WebhookHandler {
	return &WebhookHandler{queries: queries}
}

// HandlerWebhooks godocs
//
//	@Summary		Register webhook (TODO)
//	@Tags			Service accounts
//
//	@Description	register a webhook to recieve signals batch status updates
//
//	@Failure		204	{object}	responses.ErrorResponse	"Not implemented"
//
//	@Router			/webhooks [post]
func (wh *WebhookHandler) HandlerWebhooks(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - webhooks not yet implemented")
}
