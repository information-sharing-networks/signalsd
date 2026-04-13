package server

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// HomePage handles the root path and redirects to the dashboard if authenticated, login if not
func (s *Server) HomePage(w http.ResponseWriter, r *http.Request) {

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		s.RedirectToLogin(w, r)
		return
	}

	status := s.authService.CheckAccessTokenStatus(accessTokenDetails)

	switch status {
	case auth.TokenValid:
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	case auth.TokenExpired, auth.TokenInvalid, auth.TokenMissing:
		s.RedirectToLogin(w, r)
	}
}

// LoginPage godoc
//
//	@Summary	Login page
//	@Tags		UI Pages
//	@Success	200	"HTML page"
//	@Router		/login [get]
func (s *Server) LoginPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.LoginPageWithEnvironment(s.config.Environment)).ServeHTTP(w, r)
}

// Helper method for redirecting to login
func (s *Server) RedirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// Login godoc
//
//	@Summary		Authenticate user
//	@Description	HTMX endpoint. Authenticates the user and sets session cookies. On success redirects to /dashboard via HX-Redirect.
//	@Tags			HTMX Actions
//	@Param			email		formData	string	true	"User email"
//	@Param			password	formData	string	true	"User password"
//	@Success		200			"HTML partial or HX-Redirect header"
//	@Failure		400			"HTML error partial"
//	@Router			/login [post]
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	reqLogger := logger.ContextRequestLogger(r.Context())

	// Authenticate with signalsd API using the client
	accessTokenDetails, refreshTokenCookie, clientError := s.apiClient.Login(email, password)
	if clientError != nil {
		reqLogger.Error("Authentication failed", slog.String("error", clientError.Error()))

		var msg string
		if ce, ok := clientError.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	// Set all authentication cookies
	if err := s.authService.SetAuthCookies(w, accessTokenDetails, refreshTokenCookie); err != nil {
		reqLogger.Error("Failed to set authentication cookies", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("An error occurred. Please try again.")).ServeHTTP(w, r)
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

// RegisterPage godoc
//
//	@Summary	Registration page
//	@Tags		UI Pages
//	@Success	200	"HTML page"
//	@Router		/register [get]
func (s *Server) RegisterPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.RegisterPage()).ServeHTTP(w, r)
}

// Register godoc
//
//	@Summary		Register new user
//	@Description	Creates a new user account.
//	@Tags			HTMX Actions
//	@Param			email				formData	string	true	"User email"
//	@Param			password			formData	string	true	"Password"
//	@Param			confirm-password	formData	string	true	"Confirm password"
//	@Success		200					"HTML partial"
//	@Failure		400					"HTML error partial"
//	@Router			/register [post]
func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm-password")
	reqLogger := logger.ContextRequestLogger(r.Context())

	if email == "" || password == "" || confirmPassword == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all fields.")).ServeHTTP(w, r)
		return
	}

	if password != confirmPassword {
		templ.Handler(templates.ErrorAlert("Passwords do not match.")).ServeHTTP(w, r)
		return
	}

	// Register user with signalsd API
	err := s.apiClient.RegisterUser(email, password)
	if err != nil {
		reqLogger.Error("Registration failed", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	// Registration successful - show success message and redirect to login after delay
	w.Header().Set("HX-Trigger-After-Settle", "registrationSuccess")
	templ.Handler(templates.RegistrationSuccess()).ServeHTTP(w, r)
}

// Logout godoc
//
//	@Summary		Log out
//	@Description	Clears session cookies and redirects to /login.
//	@Tags			HTMX Actions
//	@Success		200	"HX-Redirect header"
//	@Router			/logout [post]
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	s.authService.ClearAuthCookies(w)

	// Redirect to login page
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}
