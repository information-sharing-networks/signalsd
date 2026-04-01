// auth package provides authentication and authorization services for the UI.
//
// The authentication flow is as follows:
//
//  1. The user enters submits the login form.
//  2. The UI calls the signalsd API /api/auth/login endpoint
//  3. The signalsd API returns an access token and a refresh token cookie.
//  4. The UI stores the access token in a cookie and the refresh token cookie in the browser.
//  5. The UI redirects the user to the home page.
//  6. The UI makes calls to the signalsd API by adding the access token to the Authorization header.
//  7. When the access token expires, the UI calls the signalsd API /oauth/token endpoint with the refresh token cookie.
//
// the middleware in this package intercepts the requests and reads cookies (access token details cookie, refresh token cookie),
// handles HTMX-aware redirects to /login or /access-denied, and checks ISN-level permissions for the web UI.
package auth
