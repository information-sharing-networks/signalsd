// Package auth provides authentication and authorisation middleware for the UI server.
//
// See server/doc.go for the full BFF token management flow.
//
// # Middleware
//
// [AuthService.RequireAuth] — validates the access token cookie on each request and
// refreshes it via the signalsd API when expired. Redirects to /login
// on failure.
//
// [AuthService.RequireRole] — checks that the authenticated account holds one of the
// specified roles; redirects to /access-denied otherwise.
//
// [AuthService.RequireIsnAdmin] — checks that the account has admin rights for at
// least one ISN; redirects to /access-denied otherwise.
//
// [AuthService.RequireIsnAccess] — checks that the account is a member of at least
// one ISN; redirects to /access-denied otherwise.
//
// # Cookie Helpers
//
// [AuthService.SetAuthCookies] writes the access token details and refresh token as
// httpOnly, SameSite=Strict cookies. [AuthService.ClearAuthCookies] expires them on logout.
//
// # Context
//
// [ContextWithAccessTokenDetails] and [ContextAccessTokenDetails] store and retrieve
// the decoded access token payload within a request context, making account identity
// and ISN permissions available to downstream handlers without re-parsing the cookie.
package auth
