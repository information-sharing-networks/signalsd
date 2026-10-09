//go:build integration

package integration

// TestAccountManagementPermissions checks the account management endpoints (disable/enable accounts, password reset links
// and reissuing service account credentials) are site admin only: they act on the account on every ISN it belongs to,
// and accounts are often members of ISNs run by different organisations.
//
// TestRevokeIsnAdminRole checks site admins can revoke the ISN admin role.
//
// TestGrantIsnAdminRole checks site admins can grant the ISN admin role, and that granting it to a site admin is rejected (it would demote them).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// sendAdminRequest sends a request with an optional json body to an admin endpoint
func sendAdminRequest(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()

	var requestBody bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&requestBody).Encode(body); err != nil {
			t.Fatalf("Failed to marshal request body: %v", err)
		}
	}

	req, err := http.NewRequest(method, url, &requestBody)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	return response
}

func TestAccountManagementPermissions(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@account-management.com")
	isnAdminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "isnadmin@account-management.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@account-management.com")

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)
	isnAdminToken := testEnv.getAccessToken(t, isnAdminAccount.ID)

	// the service account is registered by the ISN admin - registering new service accounts is still allowed for ISN admins
	registration := makeServiceAccountRegistrationRequest(t, testEnv.baseURL, isnAdminToken, serviceAccountRegistrationRequestBody{"Account Management Org", "service@account-management.com"})
	registered := expectJSONResponse(t, registration, http.StatusCreated)
	clientID := registered["client_id"].(string)

	endpoints := []struct {
		name   string
		method string
		url    string
		body   any
	}{
		{
			name:   "disable account",
			method: http.MethodPost,
			url:    fmt.Sprintf("%s/api/admin/accounts/%s/disable", testEnv.baseURL, memberAccount.ID),
		},
		{
			name:   "enable account",
			method: http.MethodPost,
			url:    fmt.Sprintf("%s/api/admin/accounts/%s/enable", testEnv.baseURL, memberAccount.ID),
		},
		{
			name:   "generate password reset link",
			method: http.MethodPost,
			url:    fmt.Sprintf("%s/api/admin/users/%s/generate-password-reset-link", testEnv.baseURL, memberAccount.ID),
		},
		{
			name:   "reissue service account credentials",
			method: http.MethodPost,
			url:    fmt.Sprintf("%s/api/auth/service-accounts/reissue-credentials", testEnv.baseURL),
			body:   reissueCredentialsRequestBody{ClientID: clientID},
		},
	}

	for _, endpoint := range endpoints {
		t.Run("isn admin cannot "+endpoint.name, func(t *testing.T) {
			expectStatus(t, sendAdminRequest(t, endpoint.method, endpoint.url, isnAdminToken, endpoint.body), http.StatusForbidden)
		})
	}

	// the member account is disabled and then re-enabled, so run these in order
	for _, endpoint := range endpoints {
		t.Run("site admin can "+endpoint.name, func(t *testing.T) {
			expectStatus(t, sendAdminRequest(t, endpoint.method, endpoint.url, siteAdminToken, endpoint.body), http.StatusOK)
		})
	}

	t.Run("isn admins can still list accounts to grant ISN access", func(t *testing.T) {
		expectStatus(t, sendAdminRequest(t, http.MethodGet, testEnv.baseURL+"/api/admin/users", isnAdminToken, nil), http.StatusOK)
		expectStatus(t, sendAdminRequest(t, http.MethodGet, testEnv.baseURL+"/api/admin/service-accounts", isnAdminToken, nil), http.StatusOK)
	})
}

func TestRevokeIsnAdminRole(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@revoke-isn-admin.com")
	otherSiteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "other-siteadmin@revoke-isn-admin.com")
	isnAdminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "isnadmin@revoke-isn-admin.com")

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	isnAdminRoleURL := func(accountID fmt.Stringer) string {
		return fmt.Sprintf("%s/api/admin/accounts/%s/isn-admin-role", testEnv.baseURL, accountID)
	}

	t.Run("site admin can revoke the ISN admin role", func(t *testing.T) {
		expectStatus(t, sendAdminRequest(t, http.MethodDelete, isnAdminRoleURL(isnAdminAccount.ID), siteAdminToken, nil), http.StatusNoContent)

		account, err := testEnv.queries.GetAccountByID(ctx, isnAdminAccount.ID)
		if err != nil {
			t.Fatalf("Failed to get account: %v", err)
		}
		if account.AccountRole != "member" {
			t.Errorf("Expected the account to be a member after the ISN admin role was revoked, got %s", account.AccountRole)
		}
	})

	t.Run("revoking the ISN admin role does not demote site admins", func(t *testing.T) {
		expectStatus(t, sendAdminRequest(t, http.MethodDelete, isnAdminRoleURL(otherSiteAdminAccount.ID), siteAdminToken, nil), http.StatusBadRequest)

		account, err := testEnv.queries.GetAccountByID(ctx, otherSiteAdminAccount.ID)
		if err != nil {
			t.Fatalf("Failed to get account: %v", err)
		}
		if account.AccountRole != "siteadmin" {
			t.Errorf("Expected the site admin to keep the site admin role, got %s", account.AccountRole)
		}
	})
}

func TestGrantIsnAdminRole(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@grant-isn-admin.com")
	otherSiteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "other-siteadmin@grant-isn-admin.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@grant-isn-admin.com")

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	isnAdminRoleURL := func(accountID fmt.Stringer) string {
		return fmt.Sprintf("%s/api/admin/accounts/%s/isn-admin-role", testEnv.baseURL, accountID)
	}

	t.Run("site admin can grant the ISN admin role to a member", func(t *testing.T) {
		expectStatus(t, sendAdminRequest(t, http.MethodPut, isnAdminRoleURL(memberAccount.ID), siteAdminToken, nil), http.StatusNoContent)

		account, err := testEnv.queries.GetAccountByID(ctx, memberAccount.ID)
		if err != nil {
			t.Fatalf("Failed to get account: %v", err)
		}
		if account.AccountRole != "isnadmin" {
			t.Errorf("Expected the account to be an ISN admin, got %s", account.AccountRole)
		}
	})

	t.Run("granting the ISN admin role does not demote site admins", func(t *testing.T) {
		expectStatus(t, sendAdminRequest(t, http.MethodPut, isnAdminRoleURL(otherSiteAdminAccount.ID), siteAdminToken, nil), http.StatusBadRequest)

		account, err := testEnv.queries.GetAccountByID(ctx, otherSiteAdminAccount.ID)
		if err != nil {
			t.Fatalf("Failed to get account: %v", err)
		}
		if account.AccountRole != "siteadmin" {
			t.Errorf("Expected the site admin to keep the site admin role, got %s", account.AccountRole)
		}
	})
}
