package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/apperrors"
	"github.com/nickabs/signals/internal/response"
)

type WebhookHandler struct {
	cfg *signals.ServiceConfig
}

func NewWebhookHandler(cfg *signals.ServiceConfig) *WebhookHandler {
	return &WebhookHandler{cfg: cfg}
}
func (wh *WebhookHandler) HandlerWebhook(w http.ResponseWriter, r *http.Request) {
	type webhookRequest struct {
		Event string `json:"event"`
		Data  struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	var req webhookRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	response.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - webhooks not yet implemented")
}
