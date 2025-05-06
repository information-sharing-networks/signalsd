package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/helpers"
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

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}
	defer r.Body.Close()
	helpers.RespondWithError(w, r, http.StatusNoContent, signals.ErrCodeNotImplemented, "todo - webhooks not yet implemented")
}
