package services

import (
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/handlers"
)

type Services struct {
	Admin        *handlers.AdminHandler
	Users        *handlers.UserHandler
	Login        *handlers.LoginHandler
	Token        *handlers.TokenHandler
	Webhook      *handlers.WebhookHandler
	SignalType   *handlers.SignalTypeHandler
	Isn          *handlers.IsnHandler
	IsnReceiver  *handlers.IsnReceiverHandler
	IsnRetriever *handlers.IsnRetrieverHandler
	AuthService  *auth.AuthService
}
