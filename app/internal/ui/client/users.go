package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// User represents a user account as returned by the signalsd API.
type User struct {
	AccountID uuid.UUID `json:"account_id"`
	Email     string    `json:"email"`
	UserRole  string    `json:"user_role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetUsers returns all user accounts
func (c *Client) GetUsers(ctx context.Context, accessToken string) ([]User, error) {
	url := fmt.Sprintf("%s/api/admin/users", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating get users request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(req, ctx)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var users []User
	if err := json.NewDecoder(res.Body).Decode(&users); err != nil {
		return nil, NewClientInternalError(err, "decoding get users response")
	}

	return users, nil
}

// RegisterUser creates a new user account using the signalsd API
func (c *Client) RegisterUser(ctx context.Context, email, password string) error {
	registerReq := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{
		Email:    email,
		Password: password,
	}

	jsonData, err := json.Marshal(registerReq) // #nosec G117 - legitimate API call, not logging secrets
	if err != nil {
		return NewClientInternalError(err, "marshaling registration request")
	}

	url := fmt.Sprintf("%s/api/auth/register", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating registration request")
	}

	req.Header.Set("Content-Type", "application/json")
	setRequestID(req, ctx)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return NewClientApiError(res)
	}

	return nil
}

type UserLookupResponse struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

// LookupUserByEmail looks up a user by email address using the admin endpoint
// Note: This requires admin permissions
func (c *Client) LookupUserByEmail(ctx context.Context, accessToken, email string) (*UserLookupResponse, error) {
	url := fmt.Sprintf("%s/api/admin/users?email=%s", c.baseURL, email)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating user lookup request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(req, ctx)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusNotFound {
			return nil, &ClientError{
				StatusCode:  http.StatusNotFound,
				UserMessage: "Email address not found.",
				LogMessage:  fmt.Sprintf("user lookup failed: email %s not found", email),
			}
		}
		return nil, NewClientApiError(res)
	}

	// Parse the single user response
	var user UserLookupResponse
	if err := json.NewDecoder(res.Body).Decode(&user); err != nil {
		return nil, NewClientInternalError(err, "decoding user lookup response")
	}

	return &user, nil
}

type GeneratePasswordResetLinkResponse struct {
	UserEmail string `json:"user_email" example:"user@example.com"`
	AccountID string `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	ResetURL  string `json:"reset_url" example:"https://api.example.com/api/auth/password-reset/0ce71234-34d5-4fb5-beb8-ad50d8b40c7d"`
	ExpiresAt string `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int    `json:"expires_in" example:"1800"`
}

// GeneratePasswordResetLink generates a password reset link for the user associated witht the supplied email
func (c *Client) GeneratePasswordResetLink(ctx context.Context, accessToken, email string) (*GeneratePasswordResetLinkResponse, error) {

	user, err := c.LookupUserByEmail(ctx, accessToken, email)
	if err != nil {
		return nil, NewClientInternalError(err, "looking up user by email")
	}

	url := fmt.Sprintf("%s/api/admin/users/%s/generate-password-reset-link", c.baseURL, user.AccountID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating generate password reset link request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")
	setRequestID(req, ctx)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var generatePasswordResetLinkResponse *GeneratePasswordResetLinkResponse
	if err := json.NewDecoder(res.Body).Decode(&generatePasswordResetLinkResponse); err != nil {
		return nil, NewClientInternalError(err, "decoding generate password reset link response")
	}

	return generatePasswordResetLinkResponse, nil
}

// GrantAdminRole grants admin role to a user account
func (c *Client) GrantAdminRole(ctx context.Context, accessToken, userEmail string) error {
	user, err := c.LookupUserByEmail(ctx, accessToken, userEmail)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/admin/accounts/%s/isn-admin-role", c.baseURL, user.AccountID)

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating grant admin role request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return NewClientApiError(res)
	}

	return nil
}

// RevokeAdminRole revokes ISN admin role from a user account
func (c *Client) RevokeAdminRole(ctx context.Context, accessToken, userEmail string) error {
	user, err := c.LookupUserByEmail(ctx, accessToken, userEmail)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/admin/accounts/%s/isn-admin-role", c.baseURL, user.AccountID)

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating revoke admin role request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return NewClientApiError(res)
	}

	return nil
}

// GrantSiteAdminRole grants site admin role to a user account
func (c *Client) GrantSiteAdminRole(ctx context.Context, accessToken, userEmail string) error {
	user, err := c.LookupUserByEmail(ctx, accessToken, userEmail)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/admin/accounts/%s/site-admin-role", c.baseURL, user.AccountID)

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating grant site admin role request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return NewClientApiError(res)
	}

	return nil
}

// RevokeSiteAdminRole revokes site admin role from a user account
func (c *Client) RevokeSiteAdminRole(ctx context.Context, accessToken, userEmail string) error {
	user, err := c.LookupUserByEmail(ctx, accessToken, userEmail)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/admin/accounts/%s/site-admin-role", c.baseURL, user.AccountID)

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating revoke site admin role request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return NewClientApiError(res)
	}

	return nil
}

// DisableAccount disables an account
func (c *Client) DisableAccount(ctx context.Context, accessToken, accountID string) error {
	url := fmt.Sprintf("%s/api/admin/accounts/%s/disable", c.baseURL, accountID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating disable account request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return NewClientApiError(res)
	}

	return nil
}

// EnableAccount enables an account
func (c *Client) EnableAccount(ctx context.Context, accessToken, accountID string) error {
	url := fmt.Sprintf("%s/api/admin/accounts/%s/enable", c.baseURL, accountID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating enable account request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return NewClientApiError(res)
	}

	return nil
}

// UpdatePassword updates the user's password (self-service)
func (c *Client) UpdatePassword(ctx context.Context, accessToken, currentPassword, newPassword string) error {
	updatePasswordReq := struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new-password"`
	}{
		CurrentPassword: currentPassword,
		NewPassword:     newPassword,
	}

	jsonData, err := json.Marshal(updatePasswordReq) // #nosec G117 - legitimate API call, not logging secrets
	if err != nil {
		return NewClientInternalError(err, "marshaling update password request")
	}

	url := fmt.Sprintf("%s/api/auth/password/reset", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating update password request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	httpReq.Header.Set("Content-Type", "application/json")
	setRequestID(httpReq, ctx)

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return NewClientApiError(res)
	}

	return nil
}
