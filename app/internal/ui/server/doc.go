// Package server contains the handlers that render the UI pages and HTMX components.
//
// Pages and components are constructed using templ templates (see the templates package).
//
// # Token Management
//
// The app implements the 'Backend for Frontend' pattern so that the browser never handles
// authentication tokens. Tokens are managed by the frontend server and injected
// into the signalsd API calls:
//
//	Browser (httpOnly cookies) -> Frontend Server (Bearer token) -> backend server (signalsd API)
//
// 1. Authentication
//
// At login the frontend server receives the access token from the signalsd API
// and stores it in an httpOnly, SameSite=Strict cookie. This ensures the token
// is never accessible to JavaScript, protecting it from XSS (Cross-Site Scripting)
// attacks.
//
// 2. Request Orchestration
//
// For every incoming request requiring data from the backend, the frontend server's
// RequireAuth middleware extracts the access token from the secure cookie and places
// it into the request context.
//
// The frontend server handlers then retrieve this token and pass it to the
// client methods in the client package, which attach it as a Bearer Authorization header
// for the signalsd API call.
//
// 3. Token Refresh
//
// At login, the signalsd API returns the refresh token as an httpOnly cookie to the
// frontend server, which re-sets it as its own httpOnly cookie in the response to the
// browser.
//
// The browser holds both the access token and the refresh token in httpOnly cookies
// and sends them with every request. When the RequireAuth middleware detects an expired
// access token, it uses the refresh token cookie to make a server-to-server exchange
// with the signalsd API, then sets new httpOnly cookies in the response. The browser
// passively holds and transmits the cookies but has no visibility into the exchange.
//
// See [auth.RequireAuth] for the middleware and [auth.SetAuthCookies] for cookie settings.
//
// # Handlers
//
// 1. UI Pages
//
// The *Page handlers render the pages at the UI's public url routes (/login, /admin/* etc).
//
// 2. HTMX Components
//
// The HTMX javascript library included in the UI pages intercepts clicks and form submissions and —
// when an action is triggered — sends an AJAX request that receives a fragment of HTML
// (e.g., a single table row or a button state) in return.
// The library then injects the partial HTML directly into the existing DOM based on the hx-target
// and hx-swap attributes in the page — this avoids full page refreshes when updating the page contents.
//
// Handlers that return HTML fragments are routed under internal /ui-api/* urls.
//
// # Retrieving Data from signalsd
//
// When a handler needs data from the backend service it should use the UI client package to call the signalsd API.
//
// # Inline JS
//
// Note the Content Security Policy on this app blocks inline javascripts and stylesheets —
// where the templates need to use JS/css add it to the app/web/js and app/web/css directories respectively.
package server
