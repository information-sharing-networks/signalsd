// package auth contains the code to handle tokens and auth middleware
//
// # Access tokens
//
// Web users are authenticated with email/password and are issued an access token and refresh token when logging in.
// The JWT access token is contained in the response body and the refresh token is in a httponly cookie.
//
// For web users, access token renewal is handled automatically by the browser sending the HttpOnly refresh token
// (the client-side application does not manually transmit or access the token).
// If the refresh token is expired or revoked the user must login again to get a new one.
//
// Service accounts use client secrets/client id.
// Service accounts are created by users with an admin role and details are shared with 3rd parties using a one-time-use link.
// Secrets must be rotated at least annually. One service account per organisation/email.
//
// Access tokens contain custom JWT claims that list the ISNs and Signal Types the account has access to.
// (for conveninence, the claims are reproduced as json in the response body returned from the /login and /oauth/refresh endpoints)
//
// Access tokens are signed with the service's secret key - the client does not need to know the secret, it is just used by the
// server to ensure the claims have not been tampered with.
//
// Access tokens expire after 30 mins.
// Refresh tokens expire after 30 days and are rotated on login or refresh request.
// These time limits are set in config.go
//
// Permissions are revalidated on the database when access tokens are refreshed, the rest of the time auth checks are done using the token claims.
//
// # isActive status
//
// marking an account  is_active= false:
//   - prevents new access tokens being created.
//   - revokes any client secrets/one time secrets (service accounts)
//   - prevents login and revokes refresh tokens (users)
//   - closes any open batches
//
// The isActive status is checked in the RequireValidClientCredentials, so handlers generally don't need to check it directly.
//
// # inUse flags
//
// ISNs can be disabled (isn.in_use = false) and signal_types can be disabled for a specific isn (isn_signal_types.in_use = false)
//
// Users will still see the disabled items in their access token claims, but they are marked in_use = false.
// The in_use flags in the claims are consulted by the RequireAccessPermission middleware - which denies access to not-in-use resources - so handlers generally don't need to check directly.
// As a fallback, the same rule is enforced at the database level (see the queries in signals.sql)
//
// # Middleware
//
// access controls are handled in middleware - make sure to call the appropriated middleware for any new routes.
//
//   - [AuthService.RequireValidAccessToken]: validates JWT signature and expiry; adds accountID, accountType and claims to context.
//
//   - [AuthService.RequireAuthForGrantType]: routes to [AuthService.RequireValidClientCredentials] or [AuthService.RequireAuthByCredentialSource]
//     based on the grant_type query param. Used on the /oauth/token endpoint.
//
//   - [AuthService.RequireAuthForCredentialType]: same routing decision but inferred from whether a refresh token cookie is present.
//     Used on the /oauth/revoke endpoint.
//
//   - [AuthService.RequireValidRefreshToken]: validates the HttpOnly refresh token cookie against the database; adds accountID and hashedRefreshToken to context.
//
//   - [AuthService.RequireValidClientCredentials]: validates client_id/client_secret from the JSON body; adds accountID to context.
//
//   - [AuthService.RequireRole]: checks the role claim matches one of the supplied roles. Must follow RequireValidAccessToken.
//
//   - [AuthService.RequireAccessPermission]: checks the account has read and/or write permission on the ISN and the ISN is in use
//     (if signal type is present in URL, it also checks the signal type is active on the ISN).
//     Use the access token claims, so must follow RequireValidAccessToken which adds the claims to the token.
//
//   - [AuthService.RequireIsnMembership]: looser than RequireAccessPermission - accepts any ISN member (read or write).
//     Use this for endpoints where write-only accounts are valid callers.
//
//   - [AuthService.RequireDevEnv]: rejects requests unless the server is running in the dev environment.
package auth
