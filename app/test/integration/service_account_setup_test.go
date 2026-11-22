//go:build integration

package integration

// Tests for the service account setup link (one-time link that exchanges a setup ID for client credentials):
// GET  /api/auth/service-accounts/setup/{setup_id} - confirmation page, does not use up the link
// POST /api/auth/service-accounts/setup/{setup_id} - issues the client secret and uses up the link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"uuid"
)

// registerServiceAccountForSetup registers a service account and returns the setup URL (on the test server) and setup ID
func registerServiceAccountForSetup(t *testing.T, testEnv *testEnv, token, organization, email string) (setupURL string, setupID uuid.UUID, accountID uuid.UUID) {
	t.Helper()

	response := makeServiceAccountRegistrationRequest(t, testEnv.baseURL, token, serviceAccountRegistrationRequestBody{organization, email})
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("failed to register service account: status %d", response.StatusCode)
	}

	var result struct {
		AccountID uuid.UUID `json:"account_id"`
		SetupURL  string    `json:"setup_url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode registration response: %v", err)
	}

	// the setup URL uses the public base URL, so rebuild it on the test server
	idString := result.SetupURL[strings.LastIndex(result.SetupURL, "/")+1:]
	setupID, err := uuid.Parse(idString)
	if err != nil {
		t.Fatalf("setup URL %q does not end with a setup ID: %v", result.SetupURL, err)
	}

	return testEnv.baseURL + "/api/auth/service-accounts/setup/" + idString, setupID, result.AccountID
}

func doSetupRequest(t *testing.T, method, setupURL string) (*http.Response, string) {
	t.Helper()
	return doSetupRequestWithHeaders(t, method, setupURL, nil)
}

func doSetupRequestWithHeaders(t *testing.T, method, setupURL string, headers map[string]string) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(method, setupURL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return response, string(body)
}

func TestServiceAccountSetupLink(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "https://testserver.com")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@setup-link-test.com")
	ownerToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	t.Run("get_does_not_use_up_the_link_and_post_issues_credentials_once", func(t *testing.T) {
		setupURL, setupID, accountID := registerServiceAccountForSetup(t, testEnv, ownerToken, "Setup Link Organization", "setup@example.com")

		oneTimeSecret, err := testEnv.queries.GetOneTimeClientSecret(ctx, setupID)
		if err != nil {
			t.Fatalf("failed to read the one-time secret: %v", err)
		}

		// link scanners and chat previews open links with GET: repeated GETs must not use up the link or show the secret
		for i := range 2 {
			response, body := doSetupRequest(t, http.MethodGet, setupURL)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %d: expected status %d, got %d", i+1, http.StatusOK, response.StatusCode)
			}
			if !strings.Contains(body, `method="POST"`) {
				t.Errorf("GET %d: expected the confirmation form", i+1)
			}
			if strings.Contains(body, oneTimeSecret.PlaintextSecret) {
				t.Errorf("GET %d: the confirmation page must not contain the client secret", i+1)
			}
		}

		active, err := testEnv.queries.CountActiveClientSecrets(ctx, accountID)
		if err != nil {
			t.Fatalf("failed to count client secrets: %v", err)
		}
		if active != 0 {
			t.Errorf("expected no client secret to be issued by GET requests, found %d", active)
		}

		// POST issues the credentials
		response, body := doSetupRequest(t, http.MethodPost, setupURL)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("POST: expected status %d, got %d", http.StatusCreated, response.StatusCode)
		}
		if !strings.Contains(body, oneTimeSecret.PlaintextSecret) {
			t.Error("POST: expected the page to contain the client secret")
		}
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("POST: expected Cache-Control no-store on the page containing the secret, got %q", got)
		}

		active, err = testEnv.queries.CountActiveClientSecrets(ctx, accountID)
		if err != nil {
			t.Fatalf("failed to count client secrets: %v", err)
		}
		if active != 1 {
			t.Errorf("expected 1 active client secret after setup, found %d", active)
		}

		// the link is now used up
		if response, _ := doSetupRequest(t, http.MethodPost, setupURL); response.StatusCode != http.StatusBadRequest {
			t.Errorf("second POST: expected status %d, got %d", http.StatusBadRequest, response.StatusCode)
		}
	})

	t.Run("concurrent_posts_issue_credentials_once", func(t *testing.T) {
		setupURL, _, accountID := registerServiceAccountForSetup(t, testEnv, ownerToken, "Concurrent Setup Organization", "concurrent-setup@example.com")

		const requests = 5
		statuses := make([]int, requests)
		var wg sync.WaitGroup
		for i := range requests {
			wg.Go(func() {
				response, _ := doSetupRequest(t, http.MethodPost, setupURL)
				statuses[i] = response.StatusCode
			})
		}
		wg.Wait()

		created := 0
		for _, status := range statuses {
			switch status {
			case http.StatusCreated:
				created++
			case http.StatusBadRequest:
			default:
				t.Errorf("unexpected status %d", status)
			}
		}
		if created != 1 {
			t.Errorf("expected exactly one request to issue credentials, got %d (statuses %v)", created, statuses)
		}

		active, err := testEnv.queries.CountActiveClientSecrets(ctx, accountID)
		if err != nil {
			t.Fatalf("failed to count client secrets: %v", err)
		}
		if active != 1 {
			t.Errorf("expected 1 active client secret, found %d", active)
		}
	})

	// the button on the confirmation page uses htmx, which replaces the page content with the response
	t.Run("htmx_requests_get_fragments", func(t *testing.T) {
		setupURL, setupID, _ := registerServiceAccountForSetup(t, testEnv, ownerToken, "Htmx Setup Organization", "htmx-setup@example.com")

		oneTimeSecret, err := testEnv.queries.GetOneTimeClientSecret(ctx, setupID)
		if err != nil {
			t.Fatalf("failed to read the one-time secret: %v", err)
		}

		// the form posts to the setup URL
		_, page := doSetupRequest(t, http.MethodGet, setupURL)
		if wantAttr := `hx-post="/api/auth/service-accounts/setup/` + setupID.String() + `"`; !strings.Contains(page, wantAttr) {
			t.Errorf("expected the confirmation page form to have %s", wantAttr)
		}

		htmx := map[string]string{"HX-Request": "true"}

		response, body := doSetupRequestWithHeaders(t, http.MethodPost, setupURL, htmx)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("POST: expected status %d, got %d", http.StatusCreated, response.StatusCode)
		}
		if strings.Contains(body, "<html") {
			t.Error("POST: expected a fragment, not a full page")
		}
		if !strings.Contains(body, oneTimeSecret.PlaintextSecret) {
			t.Error("POST: expected the fragment to contain the client secret")
		}
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("POST: expected Cache-Control no-store, got %q", got)
		}

		response, body = doSetupRequestWithHeaders(t, http.MethodPost, setupURL, htmx)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("second POST: expected status %d, got %d", http.StatusBadRequest, response.StatusCode)
		}
		if strings.Contains(body, "<html") || !strings.Contains(body, "already been used") {
			t.Errorf("second POST: expected an error fragment, got: %s", body)
		}
	})

	t.Run("invalid_setup_id", func(t *testing.T) {
		if response, _ := doSetupRequest(t, http.MethodPost, testEnv.baseURL+"/api/auth/service-accounts/setup/not-a-uuid"); response.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, response.StatusCode)
		}
	})
}
