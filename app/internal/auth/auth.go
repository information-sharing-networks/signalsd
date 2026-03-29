package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	// secretKey is the key used to sign access tokens (set by the SECRET_KEY environment variable)
	secretKey string

	// environment is the server environment ( prod, test etc - set by the ENVIRONMENT environment variable)
	environment string

	// queries is the sqlc generated database queries
	queries *database.Queries
}

func NewAuthService(secretKey string, environment string, queries *database.Queries) *AuthService {
	return &AuthService{
		secretKey:   secretKey,
		environment: environment,
		queries:     queries,
	}
}

// toSignalTypes converts internal signalTypeDetails to a map of SignalType structs keyed by path
func toSignalTypes(details []signalTypeDetails) map[string]SignalType {
	result := make(map[string]SignalType, len(details))
	for _, st := range details {
		result[st.path] = SignalType{
			Path:   st.path,
			Slug:   st.slug,
			SemVer: st.semVer,
			InUse:  st.inUse,
		}
	}
	return result
}

// AccessTokenResponse is the data returned in the response from the signalsd login and refresh token APIs
type AccessTokenResponse struct {

	// AccessToken is a JWT access token containing claims about the account and its permissions (see Claims struct)
	AccessToken string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJTaWduYWxzZCIsInN1YiI6ImMxMjQ1Yjc0LTMyMTQtNDUzOS04YTgyLTY2NDNkMzllNjk5YiIsImV4cCI6MTc0ODU4ODE2MiwiaWF0IjoxNzQ4NTg2MzYyLCJhY2NvdW50X2lkIjoiYzEyNDViNzQtMzIxNC00NTM5LThhODItNjY0M2QzOWU2OTliIiwiYWNjb3VudF90eXBlIjoidXNlciIsInJvbGUiOiJvd25lciIsImlzbl9wZXJtcyI6eyJzYW1wbGUtaXNuLS1leGFtcGxlLW9yZyI6eyJwZXJtaXNzaW9uIjoid3JpdGUiLCJzaWduYWxfdHlwZXMiOlsic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC4xIiwic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC4yIiwic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC4zIiwic2FtcGxlLXNpZ25hbG5ldy0tZXhhbXBsZS1vcmcvdjAuMC4xIiwic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC40Il19LCJzYW1wbGUtaXNuLS1zYXVsLW9yZyI6eyJwZXJtaXNzaW9uIjoid3JpdGUiLCJzaWduYWxfdHlwZXMiOlsic2FtcGxlLXNpZ25hbC0tc2F1bC1vcmcvdjAuMC4xIl19fX0.33ANor7XHWkB87npB4RWsJUjBnJHdYZce-lT8w_IN_s"`

	// TokenType (Bearer) - used as a prompt for the client to use the Bearer token type when making requests
	TokenType string `json:"token_type" example:"Bearer"`

	// ExpiresIn is the token expiry in seconds
	ExpiresIn int `json:"expires_in" example:"1800"` //seconds

	// AccountID is the account id of the user making the request
	AccountID uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`

	// AccountType is the account type of the user making the request (user or service_account)
	AccountType string `json:"account_type" enums:"user,service_account"`

	// Role is the role of the user making the request (owner, admin, member)
	Role string `json:"role" enums:"owner,admin,member" example:"admin"`

	// Perms is a map of the ISNs the account has access to and the permissions granted (the map key is the isn_slug)
	Perms map[string]IsnPerms `json:"isn_perms,omitempty"`
}

type SignalType struct {
	// Path is the signal type path in the format "slug/v{version}"
	// This is the key used in the SignalTypes map in IsnPerms
	Path string `json:"path" example:"sample-signal--example-org/v0.0.1"`

	// Slug is the signal type slug (unique per site)
	Slug string `json:"slug" example:"sample-signal--example-org"`

	// SemVer is the signal type version (e.g. 0.0.1)
	SemVer string `json:"sem_ver" example:"0.0.1"`

	// InUse is true if the signal type is active
	InUse bool `json:"in_use" example:"true"`
}

type IsnPerms struct {

	// CanRead is true if the account has read access to the isn
	CanRead bool `json:"can_read" example:"true"`

	// CanWrite is true if the account has write access to the isn
	CanWrite bool `json:"can_write" example:"false"`

	// SignalBatchID is the ID of the current signal batch for the ISN (use when writing to the isn)
	SignalBatchID *uuid.UUID `json:"signal_batch_id,omitempty" example:"967affe9-5628-4fdd-921f-020051344a12"`

	// SignalTypes is a map of the signal type paths to the signal type details (key is the signal type path)
	SignalTypes map[string]SignalType `json:"signal_types,omitempty"`

	// Visibility is the ISN visibility setting (public or private)
	Visibility string `json:"visibility" enums:"public,private" example:"private"`

	// AccountIsIsnAdmin is true if the account is the owner of the isn or the site owner
	AccountIsIsnAdmin bool `json:"account_is_isn_admin" example:"false"`

	// InUse is true if the isn is active
	InUse bool `json:"in_use" example:"true"`
}

// Claims are the claims included in the access token
type Claims struct {

	// RegisteredClaims are the standard JWT claims
	jwt.RegisteredClaims

	// AccountID is the account id of the user making the request
	AccountID uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`

	// AccountType is the account type of the user making the request (user or service_account)
	AccountType string `json:"account_type" enums:"user,service_account"`

	// Role is the role of the user making the request (owner, admin, member)
	Role string `json:"role" enums:"owner,admin,member" example:"admin"`

	// IsnPerms is a map of the ISNs and signal types the account has access to and the permissions they have been granted (the map key is the isn slug)
	IsnPerms map[string]IsnPerms `json:"isn_perms,omitempty" example:"isn1"`
}

// stucts to hold the full list of isns and signal types - used when generating the access token claims
// the items are filtered by the claims builder to only include the items the account has access to
type isnDetails struct {
	userAccountID uuid.UUID
	inUse         bool
	visibility    string
	signalTypes   []signalTypeDetails
}

type signalTypeDetails struct {
	path   string
	slug   string
	semVer string
	inUse  bool
}

type isnList map[string]*isnDetails // key is the isn slug

// create a JWT access token signed with HS256 using the app's secret key.
//
// Roles and ISN read/write permissions are retreived from the database and included in the token claims and the response body.
//
// the access token contains:
//   - standard jwt registerd claims(sub, exp, iat)
//   - account ID
//   - account type (user or service_account)
//   - account role (owner, admin, member)
//   - A list of all the isns the account has access to and the permission granted (read or write)
//   - the list of available signal_types in the isn
//
// note inactive isns/signal_types are included - an is_in_use flag is included in the claims so the client can make access decisions
//
// The function returns the token inside a AccessTokenResponse that can be returned to the client.
//
// if this function generates an error, it is unexpected and the calling handler should produce a 500 status code
//
//		this function is only used when the user logs-in or when an account refreshes an access token.
//		Since the calling functions authenticate using secrets that (should) only be known by the client,
//	 the claims in the token can be trusted by the handler without rechecking the database
//
// Caveat:
//
//	Note that since the tokens last 30 mins, there is the potential for the permissions to become stale.
//	if there are particular requests that *must* have the latest permissions the handler should check the db rather than using the claims info.
func (a *AuthService) CreateAccessToken(ctx context.Context) (AccessTokenResponse, error) {

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(signalsd.AccessTokenExpiry)
	isnPerms := make(map[string]IsnPerms) // key is the isn slug

	accountID, ok := ContextAccountID(ctx)
	if !ok {
		return AccessTokenResponse{}, fmt.Errorf("unexpected error - accountID not in context")
	}

	//get user role
	account, err := a.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AccessTokenResponse{}, fmt.Errorf("user not found: %v", accountID)
		}
		return AccessTokenResponse{}, fmt.Errorf("database error getting user %v: %w", accountID, err)
	}

	// this should be caught by the middleware, but double check here in case of bugs elsewhere
	if !account.IsActive {
		return AccessTokenResponse{}, fmt.Errorf("account %v is disabled", accountID)
	}

	if !signalsd.ValidRoles[account.AccountRole] {
		return AccessTokenResponse{}, fmt.Errorf("invalid user role %v for user %v", account.AccountRole, accountID)
	}

	// get all the isns on the site
	dbIsnList, err := a.queries.GetIsns(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AccessTokenResponse{}, fmt.Errorf("database error getting ISNs: %w", err)
	}

	isnList := make(isnList)

	for _, dbIsn := range dbIsnList {

		isnList[dbIsn.Slug] = &isnDetails{
			userAccountID: dbIsn.UserAccountID,
			inUse:         dbIsn.IsInUse,
			visibility:    dbIsn.Visibility,
		}

		// get the signal types for this isn
		dbSignalTypes, err := a.queries.GetSignalTypesByIsnID(ctx, dbIsn.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return AccessTokenResponse{}, fmt.Errorf("database error getting signal_types: %w", err)
		}

		signalTypes := make([]signalTypeDetails, 0)
		for _, dbSignalType := range dbSignalTypes {

			signalTypes = append(signalTypes, signalTypeDetails{
				path:   fmt.Sprintf("%s/v%s", dbSignalType.Slug, dbSignalType.SemVer),
				slug:   dbSignalType.Slug,
				semVer: dbSignalType.SemVer,
				inUse:  dbSignalType.IsInUse,
			})
		}
		isnList[dbIsn.Slug].signalTypes = signalTypes

	}

	// get the isns this account's has been granted access to.
	isnsAccessibleByAccount, err := a.queries.GetIsnAccountsByAccountID(ctx, accountID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AccessTokenResponse{}, fmt.Errorf("database error getting ISN accounts: %w", err)
	}

	//create a map of isn_slug to the account's open batch for the isn
	latestSignalBatches, err := a.queries.GetLatestIsnSignalBatchesByAccountID(ctx, accountID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AccessTokenResponse{}, fmt.Errorf("database error %w", err)
	}

	latestSignalBatchIDs := make(map[string]*uuid.UUID)
	for _, batch := range latestSignalBatches {
		latestSignalBatchIDs[batch.IsnSlug] = &batch.ID
	}

	// set up isnPerms map for claims
	switch account.AccountRole {
	case "owner":
		// owner has read and write access to all ISNs
		for isnSlug, siteIsn := range isnList {
			isnPerms[isnSlug] = IsnPerms{
				CanRead:           true,
				CanWrite:          true,
				SignalBatchID:     latestSignalBatchIDs[isnSlug],
				SignalTypes:       toSignalTypes(siteIsn.signalTypes),
				Visibility:        siteIsn.visibility,
				InUse:             siteIsn.inUse,
				AccountIsIsnAdmin: true,
			}
		}

	case "admin":
		// Admin can read, write and administrate any ISN they created
		for isnSlug, siteIsn := range isnList {
			if account.ID == siteIsn.userAccountID {
				isnPerms[isnSlug] = IsnPerms{
					CanRead:           true,
					CanWrite:          true,
					SignalBatchID:     latestSignalBatchIDs[isnSlug],
					SignalTypes:       toSignalTypes(siteIsn.signalTypes),
					Visibility:        siteIsn.visibility,
					InUse:             siteIsn.inUse,
					AccountIsIsnAdmin: true,
				}
			}
		}
		// and access any ISN where they were granted read or write permission by another admin or the owner
		for _, accessibleIsn := range isnsAccessibleByAccount {
			isnSlug := accessibleIsn.IsnSlug
			if _, ok := isnPerms[isnSlug]; !ok {
				isnPerms[isnSlug] = IsnPerms{
					CanRead:           accessibleIsn.CanRead,
					CanWrite:          accessibleIsn.CanWrite,
					SignalBatchID:     latestSignalBatchIDs[isnSlug],
					SignalTypes:       toSignalTypes(isnList[isnSlug].signalTypes),
					Visibility:        isnList[isnSlug].visibility,
					InUse:             isnList[isnSlug].inUse,
					AccountIsIsnAdmin: false,
				}
			}
		}

	case "member":
		// Member only has granted permissions (service accounts are always treated as members)
		for _, accessibleIsn := range isnsAccessibleByAccount {
			isnSlug := accessibleIsn.IsnSlug
			isnPerms[isnSlug] = IsnPerms{
				CanRead:           accessibleIsn.CanRead,
				CanWrite:          accessibleIsn.CanWrite,
				SignalBatchID:     latestSignalBatchIDs[isnSlug],
				SignalTypes:       toSignalTypes(isnList[isnSlug].signalTypes),
				Visibility:        isnList[isnSlug].visibility,
				InUse:             isnList[isnSlug].inUse,
				AccountIsIsnAdmin: false,
			}
		}

	default:
		return AccessTokenResponse{}, fmt.Errorf("unexpected role : %v", account.AccountRole)
	}

	// claims
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    signalsd.TokenIssuerName,
		},
		AccountID:   account.ID,
		AccountType: account.AccountType,
		Role:        account.AccountRole,
		IsnPerms:    isnPerms,
	}

	// create a new signed token
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedAccessToken, err := accessToken.SignedString([]byte(a.secretKey))
	if err != nil {
		return AccessTokenResponse{}, fmt.Errorf("failed to sign JWT: %w", err)
	}
	return AccessTokenResponse{
		AccessToken: signedAccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(signalsd.AccessTokenExpiry.Seconds()),
		AccountID:   account.ID,
		AccountType: account.AccountType,
		Role:        account.AccountRole,
		Perms:       isnPerms,
	}, nil
}

// rerturn the JWT access token from Authorization header
func (a *AuthService) GetAccessTokenFromHeader(headers http.Header) (string, error) {
	authorizationHeaderValue := headers.Get("Authorization")
	if authorizationHeaderValue == "" {
		return "", fmt.Errorf("authorization header is missing")
	}

	parts := strings.Fields(authorizationHeaderValue)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", fmt.Errorf("authorization header format must be Bearer {token}")
	}

	return parts[1], nil
}

// revoke any open refresh tokens for the user contained in the shared context
// stores the hashed token
// returns the new token as plain text
func (a *AuthService) RotateRefreshToken(ctx context.Context) (string, error) {
	userAccountID, ok := ContextAccountID(ctx)
	if !ok {
		return "", fmt.Errorf("authservice: did not receive userAccountID from middleware")
	}

	_, err := a.queries.RevokeAllRefreshTokensForUser(ctx, userAccountID)
	if err != nil {
		return "", fmt.Errorf("authservice: could not revoke previous refresh tokens for user %v", userAccountID)
	}

	plainTextToken, err := a.GenerateSecureToken(32)
	if err != nil {
		return "", fmt.Errorf("authservice: error creating refresh token: %v", err)
	}

	// Hash the plain text token
	hashedToken := a.HashToken(plainTextToken)

	// store the hashed value
	_, err = a.queries.InsertRefreshToken(ctx, database.InsertRefreshTokenParams{
		HashedToken:   hashedToken,
		UserAccountID: userAccountID,
		ExpiresAt:     time.Now().Add(signalsd.RefreshTokenExpiry),
	})
	if err != nil {
		return "", fmt.Errorf("authservice: could not insert refresh token: %v", err)
	}

	return plainTextToken, nil
}
func (a *AuthService) NewRefreshTokenCookie(refreshToken string) *http.Cookie {

	isProdOrStaging := a.environment == "prod" || a.environment == "staging" //secure flag only true on prod and staging

	// #nosec G124 - Secure flag is conditionally true on prod/staging
	newCookie := &http.Cookie{
		Name:     signalsd.RefreshTokenCookieName,
		Value:    refreshToken,
		Path:     "/oauth",
		MaxAge:   int(signalsd.RefreshTokenExpiry.Seconds()),
		HttpOnly: true,
		Secure:   isProdOrStaging,
		SameSite: http.SameSiteStrictMode,
	}

	return newCookie
}

func (a *AuthService) HashPassword(password string) (string, error) {
	dat, err := bcrypt.GenerateFromPassword([]byte(password), signalsd.BcryptCost)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}

// CheckPasswordHash compares a bcrypt hashed password with its possible plaintext equivalent.
func (a *AuthService) CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GenerateSecureToken
// Returns the token as a base64-URL-encoded string for safe transmission/storage
func (a *AuthService) GenerateSecureToken(byteLength int) (string, error) {
	tokenBytes := make([]byte, byteLength)
	_, err := io.ReadFull(rand.Reader, tokenBytes)
	if err != nil {
		return "", fmt.Errorf("error generating secure random bytes: %v", err)
	}

	return base64.URLEncoding.EncodeToString(tokenBytes), nil
}

// hash a token using sha512
func (a *AuthService) HashToken(token string) string {
	hasher := sha512.New()
	hasher.Write([]byte(token))
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}
