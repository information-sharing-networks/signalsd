// the ui handlers package handle the ui-api routes
package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// HomePage handles the root path and redirects to the dashboard if authenticated, login if not
func (h *HandlerService) HomePage(w http.ResponseWriter, r *http.Request) {

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		h.RedirectToLogin(w, r)
		return
	}

	status := h.AuthService.CheckAccessTokenStatus(accessTokenDetails)

	switch status {
	case auth.TokenValid:
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	case auth.TokenExpired, auth.TokenInvalid, auth.TokenMissing:
		h.RedirectToLogin(w, r)
	}
}

func (h *HandlerService) LoginPage(w http.ResponseWriter, r *http.Request) {
	// Render login page
	component := templates.LoginPage()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render login page", slog.String("error", err.Error()))
	}
}

// Helper method for redirecting to login
func (h *HandlerService) RedirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// Login authenticates the user and adds authentication cookies to the response
func (h *HandlerService) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	reqLogger := logger.ContextRequestLogger(r.Context())

	// Authenticate with signalsd API using the client
	accessTokenDetails, refreshTokenCookie, clientError := h.ApiClient.Login(email, password)
	if clientError != nil {
		reqLogger.Error("Authentication failed", slog.String("error", clientError.Error()))

		var msg string
		if ce, ok := clientError.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Set all authentication cookies
	if err := h.AuthService.SetAuthCookies(w, accessTokenDetails, refreshTokenCookie); err != nil {
		reqLogger.Error("Failed to set authentication cookies", slog.String("error", err.Error()))

		component := templates.ErrorAlert("An error occurred. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	//  add account log attribute to context so it is included in the final request log
	_ = logger.ContextWithLogAttrs(r.Context(),
		slog.String("account_id", accessTokenDetails.AccountID),
	)

	// redirect to dashboard
	w.Header().Set("HX-Redirect", "/dashboard")
	w.WriteHeader(http.StatusOK)
}

func (h *HandlerService) RegisterPage(w http.ResponseWriter, r *http.Request) {
	// Render registration page
	component := templates.RegisterPage()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render registration page", slog.String("error", err.Error()))
	}
}

// Register processes user registration
func (h *HandlerService) Register(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm-password")
	reqLogger := logger.ContextRequestLogger(r.Context())

	if email == "" || password == "" || confirmPassword == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	if password != confirmPassword {
		component := templates.ErrorAlert("Passwords do not match.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Register user with signalsd API
	err := h.ApiClient.RegisterUser(email, password)
	if err != nil {
		reqLogger.Error("Registration failed", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Registration successful - show success message and redirect to login after delay
	w.Header().Set("HX-Trigger-After-Settle", "registrationSuccess")
	component := templates.RegistrationSuccess()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render registration success", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) Logout(w http.ResponseWriter, r *http.Request) {
	h.AuthService.ClearAuthCookies(w)

	// Redirect to login page
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}
