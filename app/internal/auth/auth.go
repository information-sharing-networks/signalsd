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
	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	secretKey   string
	environment string
	queries     *database.Queries
}

func NewAuthService(secretKey string, environment string, queries *database.Queries) *AuthService {
	return &AuthService{
		secretKey:   secretKey,
		environment: environment,
		queries:     queries,
	}
}

type AccessTokenResponse struct {
	AccessToken string              `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJTaWduYWxzZCIsInN1YiI6ImMxMjQ1Yjc0LTMyMTQtNDUzOS04YTgyLTY2NDNkMzllNjk5YiIsImV4cCI6MTc0ODU4ODE2MiwiaWF0IjoxNzQ4NTg2MzYyLCJhY2NvdW50X2lkIjoiYzEyNDViNzQtMzIxNC00NTM5LThhODItNjY0M2QzOWU2OTliIiwiYWNjb3VudF90eXBlIjoidXNlciIsInJvbGUiOiJvd25lciIsImlzbl9wZXJtcyI6eyJzYW1wbGUtaXNuLS1leGFtcGxlLW9yZyI6eyJwZXJtaXNzaW9uIjoid3JpdGUiLCJzaWduYWxfdHlwZXMiOlsic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC4xIiwic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC4yIiwic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC4zIiwic2FtcGxlLXNpZ25hbG5ldy0tZXhhbXBsZS1vcmcvdjAuMC4xIiwic2FtcGxlLXNpZ25hbC0tZXhhbXBsZS1vcmcvdjAuMC40Il19LCJzYW1wbGUtaXNuLS1zYXVsLW9yZyI6eyJwZXJtaXNzaW9uIjoid3JpdGUiLCJzaWduYWxfdHlwZXMiOlsic2FtcGxlLXNpZ25hbC0tc2F1bC1vcmcvdjAuMC4xIl19fX0.33ANor7XHWkB87npB4RWsJUjBnJHdYZce-lT8w_IN_s"`
	TokenType   string              `json:"token_type" example:"Bearer"`
	ExpiresIn   int                 `json:"expires_in" example:"1800"` //seconds
	AccountID   uuid.UUID           `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	AccountType string              `json:"account_type" enums:"user,service_account"`
	Role        string              `json:"role" enums:"owner,admin,member" example:"admin"`
	Perms       map[string]IsnPerms `json:"isn_perms,omitempty"`
}

type IsnPerms struct {
	Permission      string     `json:"permission" enums:"read,write" example:"read"`
	SignalBatchID   *uuid.UUID `json:"signal_batch_id,omitempty" example:"967affe9-5628-4fdd-921f-020051344a12"`
	SignalTypePaths []string   `json:"signal_types,omitempty" example:"signal-type-1/v0.0.1,signal-type-2/v1.0.0"` // list of available signal types for the isn
}

type AccessTokenClaims struct {
	jwt.RegisteredClaims
	AccountID   uuid.UUID           `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	AccountType string              `json:"account_type" enums:"user,service_account"`
	Role        string              `json:"role" enums:"owner,admin,member" example:"admin"`
	IsnPerms    map[string]IsnPerms `json:"isn_perms,omitempty" example:"isn1"`
}

func (a AuthService) HashPassword(password string) (string, error) {
	dat, err := bcrypt.GenerateFromPassword([]byte(password), signalsd.BcryptCost)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}

func (a AuthService) CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GenerateSecureToken
// Returns the token as a base64-URL-encoded string for safe transmission/storage
func (a AuthService) GenerateSecureToken(byteLength int) (string, error) {
	tokenBytes := make([]byte, byteLength)
	_, err := io.ReadFull(rand.Reader, tokenBytes)
	if err != nil {
		return "", fmt.Errorf("error generating secure random bytes: %v", err)
	}

	return base64.URLEncoding.EncodeToString(tokenBytes), nil
}

// hash a token using sha512
func (a AuthService) HashToken(token string) string {
	hasher := sha512.New()
	hasher.Write([]byte(token))
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}

// check that the hashed value of a token is the same as the supplied hash
func (a AuthService) CheckTokenHash(hash string, token string) bool {
	return hash == a.HashToken(token)
}

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
// The function returns the token inside a AccessTokenResponse that can be returned to the client.
//
// if this function generates an error, it is unexpected and the calling handler should produce a 500 status code
//
//	this function is only used when logging in or refreshing an access token.
//	Since the calling functions authenticate using secrets that (should) only be known by the client, the claims in the token can be trusted by the handler without rechecking the database
//
// Caveat:
//
//	Note that since the tokens last 30 mins, there is the potential for the permissions to become stale.
//	if there are particular requests that *must* have the latest permissions the handler should check the db rather than using the claims info.
func (a AuthService) CreateAccessToken(ctx context.Context) (AccessTokenResponse, error) {

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(signalsd.AccessTokenExpiry)
	isnPerms := make(map[string]IsnPerms)

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

	if !signalsd.ValidRoles[account.AccountRole] {
		return AccessTokenResponse{}, fmt.Errorf("invalid user role %v for user %v", account.AccountRole, accountID)
	}

	// get all the Isns on this site
	siteIsns, err := a.queries.GetIsns(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AccessTokenResponse{}, fmt.Errorf("database error getting ISNs: %w", err)
	}

	// create a map of theIsns with their available signal_type paths (sample-signal--example-org/0.0.1 etc)
	// this list is included in the claims assuming the user has permission for the isn
	siteIsnsSignalTypePaths := make(map[string][]string)
	for _, isn := range siteIsns {

		signalTypeRows, err := a.queries.GetInUseSignalTypesByIsnID(ctx, isn.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return AccessTokenResponse{}, fmt.Errorf("database error getting signal_types: %w", err)
		}

		signalTypePaths := make([]string, 0)
		for _, signalType := range signalTypeRows {
			ver := fmt.Sprintf("%s/v%s", signalType.Slug, signalType.SemVer)
			signalTypePaths = append(signalTypePaths, ver)
		}

		siteIsnsSignalTypePaths[isn.Slug] = signalTypePaths
	}

	// get the isns this account's has access to.
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
		// owner can write to all ISNs
		for _, siteIsn := range siteIsns {
			isnPerms[siteIsn.Slug] = IsnPerms{
				Permission:      "write",
				SignalBatchID:   latestSignalBatchIDs[siteIsn.Slug],
				SignalTypePaths: siteIsnsSignalTypePaths[siteIsn.Slug],
			}
		}

	case "admin":
		// Admin can write to any ISN they created
		for _, siteIsn := range siteIsns {
			if account.ID == siteIsn.UserAccountID {
				isnPerms[siteIsn.Slug] = IsnPerms{
					Permission:      "write",
					SignalBatchID:   latestSignalBatchIDs[siteIsn.Slug],
					SignalTypePaths: siteIsnsSignalTypePaths[siteIsn.Slug],
				}
			}
		}
		//.. and access any ISN where they were granted read or write permission by the isn owner

		for _, accessibleIsn := range isnsAccessibleByAccount {
			isnPerms[accessibleIsn.IsnSlug] = IsnPerms{
				Permission:      accessibleIsn.Permission,
				SignalBatchID:   latestSignalBatchIDs[accessibleIsn.IsnSlug],
				SignalTypePaths: siteIsnsSignalTypePaths[accessibleIsn.IsnSlug],
			}
		}
	case "member":
		// Member only has granted permissions (not service identites are always treated as members)
		for _, accessibleIsn := range isnsAccessibleByAccount {
			isnPerms[accessibleIsn.IsnSlug] = IsnPerms{
				Permission:      accessibleIsn.Permission,
				SignalBatchID:   latestSignalBatchIDs[accessibleIsn.IsnSlug],
				SignalTypePaths: siteIsnsSignalTypePaths[accessibleIsn.IsnSlug],
			}
		}
	default:
		return AccessTokenResponse{}, fmt.Errorf("unexpected role : %v", account.AccountRole)
	}

	// claims
	claims := AccessTokenClaims{
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
func (a AuthService) GetAccessTokenFromHeader(headers http.Header) (string, error) {
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
func (a AuthService) RotateRefreshToken(ctx context.Context) (string, error) {
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
func (a AuthService) NewRefreshTokenCookie(environment string, refreshToken string) *http.Cookie {

	isProd := a.environment == "prod" //Secure flag is only true on prod

	newCookie := &http.Cookie{
		Name:     signalsd.RefreshTokenCookieName,
		Value:    refreshToken,
		Path:     "/auth",
		Expires:  time.Now().Add(signalsd.RefreshTokenExpiry),
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	}

	return newCookie
}
