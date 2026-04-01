package handlers

import (
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
)

// HandlerService provides the dependencies for the UI handlers
type HandlerService struct {

	// AuthService provides authentication and authorization services
	AuthService *auth.AuthService

	// ApiClient provides a client for calling the signalsd API
	ApiClient *client.Client

	// Environment is the server environment (dev, test, perf, staging, prod)
	Environment string
}
