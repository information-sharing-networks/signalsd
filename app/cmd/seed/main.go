// seed is a CLI tool for populating a dev environment with test data for exploratory testing.
// It assumes the app is running at http://localhost:8080 with an empty database.
// AI generated - testing only
//
// Usage:
//
//	go run ./cmd/seed
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"
)

const (
	baseURL  = "http://localhost:8080"
	password = "12345678901"

	testSignalTypeDetail = "Simple test signal type for integration tests"
	testSchemaURL        = "https://github.com/information-sharing-networks/signal-library/blob/main/signalsd-testing/simple.json"
	testReadmeURL        = "https://github.com/information-sharing-networks/signal-library/blob/main/signalsd-testing/README.md"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func main() {
	// 1. Create user accounts
	fmt.Println("=== Creating user accounts ===")
	doPost("/api/auth/register", "", map[string]string{"email": "owner@gmail.com", "password": password}, 201)
	fmt.Println("  ✓ owner@gmail.com")
	doPost("/api/auth/register", "", map[string]string{"email": "admin@gmail.com", "password": password}, 201)
	fmt.Println("  ✓ admin@gmail.com")
	doPost("/api/auth/register", "", map[string]string{"email": "user1@gmail.com", "password": password}, 201)
	fmt.Println("  ✓ user1@gmail.com")
	doPost("/api/auth/register", "", map[string]string{"email": "user2@gmail.com", "password": password}, 201)
	fmt.Println("  ✓ user2@gmail.com")

	// Login as owner (siteadmin — first registered user)
	ownerToken := loginUser("owner@gmail.com", password)

	// Get account IDs for other users
	adminID := getUserIDByEmail(ownerToken, "admin@gmail.com")
	user1ID := getUserIDByEmail(ownerToken, "user1@gmail.com")
	user2ID := getUserIDByEmail(ownerToken, "user2@gmail.com")

	// Grant admin the isnadmin role
	doPut(fmt.Sprintf("/api/admin/accounts/%s/isn-admin-role", adminID), ownerToken, nil, 204)
	fmt.Println("  ✓ Granted isnadmin role to admin@gmail.com")

	// 2. Create service accounts (requires isnadmin/siteadmin)
	fmt.Println("\n=== Creating service accounts ===")
	sa1 := createServiceAccount(ownerToken, "google", "service1@gmail.com")
	sa1ClientID, sa1Secret := completeServiceAccountSetup(sa1.SetupURL)
	fmt.Printf("  ✓ service1@gmail.com (google) - client_id: %s\n", sa1ClientID)

	sa2 := createServiceAccount(ownerToken, "google", "service2@gmail.com")
	sa2ClientID, sa2Secret := completeServiceAccountSetup(sa2.SetupURL)
	fmt.Printf("  ✓ service2@gmail.com (google) - client_id: %s\n", sa2ClientID)

	// 3. Create ISNs (as admin)
	fmt.Println("\n=== Creating ISNs ===")
	adminToken := loginUser("admin@gmail.com", password)

	detail := "Test ISN for exploratory testing"
	isInUse := true
	visibility := "private"
	isna := doPost("/api/isn/", adminToken, map[string]any{
		"title":      "isn-a",
		"detail":     detail,
		"is_in_use":  isInUse,
		"visibility": visibility,
	}, 201)
	isnaSlug := isna["slug"].(string)
	fmt.Printf("  ✓ %s\n", isnaSlug)

	isnb := doPost("/api/isn/", adminToken, map[string]any{
		"title":      "isn-b",
		"detail":     detail,
		"is_in_use":  isInUse,
		"visibility": visibility,
	}, 201)
	isnbSlug := isnb["slug"].(string)
	fmt.Printf("  ✓ %s\n", isnbSlug)

	// 4. Create signal types (requires siteadmin)
	fmt.Println("\n=== Creating signal types ===")
	singalTypea := doPost("/api/admin/signal-types", ownerToken, map[string]string{
		"schema_url": testSchemaURL,
		"title":      "signaltype-a",
		"bump_type":  "patch",
		"readme_url": testReadmeURL,
		"detail":     testSignalTypeDetail,
	}, 201)
	singalTypeaSlug := singalTypea["slug"].(string)
	singalTypeaSemVer := singalTypea["sem_ver"].(string)
	fmt.Printf("  ✓ %s v%s\n", singalTypeaSlug, singalTypeaSemVer)

	singalTypeb := doPost("/api/admin/signal-types", ownerToken, map[string]string{
		"schema_url": testSchemaURL,
		"title":      "signaltype-b",
		"bump_type":  "patch",
		"readme_url": testReadmeURL,
		"detail":     testSignalTypeDetail,
	}, 201)
	singalTypebSlug := singalTypeb["slug"].(string)
	singalTypebSemVer := singalTypeb["sem_ver"].(string)
	fmt.Printf("  ✓ %s v%s\n", singalTypebSlug, singalTypebSemVer)

	// 5. Add signal types to ISNs
	fmt.Println("\n=== Adding signal types to ISNs ===")
	for _, isnSlug := range []string{isnaSlug, isnbSlug} {
		doPost(fmt.Sprintf("/api/isn/%s/signal-types/add", isnSlug), adminToken, map[string]string{
			"signal_type_slug": singalTypeaSlug,
			"sem_ver":          singalTypeaSemVer,
		}, 204)
		doPost(fmt.Sprintf("/api/isn/%s/signal-types/add", isnSlug), adminToken, map[string]string{
			"signal_type_slug": singalTypebSlug,
			"sem_ver":          singalTypebSemVer,
		}, 204)
		fmt.Printf("  ✓ Added %s and %s to %s\n", singalTypeaSlug, singalTypebSlug, isnSlug)
	}

	// 6. Grant permissions
	fmt.Println("\n=== Granting ISN permissions ===")
	for _, isnSlug := range []string{isnaSlug, isnbSlug} {
		// user1 and service1: read + write
		grantPermission(adminToken, isnSlug, user1ID, true, true)
		grantPermission(adminToken, isnSlug, sa1.AccountID, true, true)
		// user2 and service2: read only
		grantPermission(adminToken, isnSlug, user2ID, true, false)
		grantPermission(adminToken, isnSlug, sa2.AccountID, true, false)
		fmt.Printf("  ✓ Permissions set for %s\n", isnSlug)
	}

	// Summary
	fmt.Println("\n=== Setup complete ===")
	fmt.Println("\nUser accounts (password: " + password + "):")
	fmt.Println("  owner@gmail.com  — siteadmin")
	fmt.Println("  admin@gmail.com  — isnadmin")
	fmt.Println("  user1@gmail.com  — member (read+write on isna, isnb)")
	fmt.Println("  user2@gmail.com  — member (read-only on isna, isnb)")
	fmt.Println("\nService accounts:")
	fmt.Printf("  service1@gmail.com (google) — read+write on isna, isnb\n    client_id:     %s\n    client_secret: %s\n", sa1ClientID, sa1Secret)
	fmt.Printf("  service2@gmail.com (google) — read-only on isna, isnb\n    client_id:     %s\n    client_secret: %s\n", sa2ClientID, sa2Secret)
	fmt.Printf("\nISNs: %s, %s\n", isnaSlug, isnbSlug)
	fmt.Printf("Signal types: %s v%s, %s v%s\n", singalTypeaSlug, singalTypeaSemVer, singalTypebSlug, singalTypebSemVer)
}

type serviceAccountResponse struct {
	ClientID  string `json:"client_id"`
	AccountID string `json:"account_id"`
	SetupURL  string `json:"setup_url"`
}

func loginUser(email, pass string) string {
	resp := doPost("/api/auth/login", "", map[string]string{
		"email":    email,
		"password": pass,
	}, 200)
	token, ok := resp["access_token"].(string)
	if !ok || token == "" {
		fatalf("login failed for %s: no access_token in response", email)
	}
	return token
}

func getUserIDByEmail(token, email string) string {
	resp := doGet(fmt.Sprintf("/api/admin/users?email=%s", email), token, 200)
	id, ok := resp["account_id"].(string)
	if !ok || id == "" {
		fatalf("could not get account ID for %s", email)
	}
	return id
}

func createServiceAccount(token, org, email string) serviceAccountResponse {
	body := doPost("/api/auth/service-accounts/register", token, map[string]string{
		"client_organization":  org,
		"client_contact_email": email,
	}, 201)

	var sa serviceAccountResponse
	b, _ := json.Marshal(body)
	if err := json.Unmarshal(b, &sa); err != nil {
		fatalf("could not parse service account response: %v", err)
	}
	if sa.SetupURL == "" {
		fatalf("no setup_url in service account response for %s", email)
	}
	return sa
}

// completeServiceAccountSetup visits the setup URL and extracts credentials from the HTML response.
// The setup page contains data-text attributes with the client_id and client_secret values.
func completeServiceAccountSetup(setupURL string) (clientID, clientSecret string) {
	resp, err := httpClient.Get(setupURL)
	if err != nil {
		fatalf("failed to call setup URL %s: %v", setupURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fatalf("failed to read setup response body: %v", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		fatalf("unexpected status %d calling setup URL %s:\n%s", resp.StatusCode, setupURL, body)
	}

	// Extract all data-text="..." attribute values from the HTML.
	// The page has two: client_id first, then client_secret.
	re := regexp.MustCompile(`data-text="([^"]+)"`)
	matches := re.FindAllSubmatch(body, -1)
	if len(matches) < 2 {
		fatalf("could not extract credentials from setup page (found %d data-text attributes):\n%s", len(matches), body)
	}
	return string(matches[0][1]), string(matches[1][1])
}

func grantPermission(token, isnSlug, accountID string, canRead, canWrite bool) {
	doPut(fmt.Sprintf("/api/isn/%s/accounts/%s", isnSlug, accountID), token, map[string]bool{
		"can_read":  canRead,
		"can_write": canWrite,
	}, 200)
}

// doPost sends a JSON POST and returns the decoded response body.
func doPost(path, token string, body any, expectStatus int) map[string]any {
	return doRequest("POST", path, token, body, expectStatus)
}

// doPut sends a JSON PUT and returns the decoded response body.
func doPut(path, token string, body any, expectStatus int) map[string]any {
	return doRequest("PUT", path, token, body, expectStatus)
}

// doGet sends a GET and returns the decoded response body.
func doGet(path, token string, expectStatus int) map[string]any {
	return doRequest("GET", path, token, nil, expectStatus)
}

func doRequest(method, path, token string, body any, expectStatus int) map[string]any {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			fatalf("failed to marshal request body for %s %s: %v", method, path, err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, baseURL+path, reqBody)
	if err != nil {
		fatalf("failed to create request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		fatalf("request failed %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fatalf("failed to read response body for %s %s: %v", method, path, err)
	}

	if resp.StatusCode != expectStatus {
		fatalf("%s %s: expected status %d, got %d\nresponse: %s", method, path, expectStatus, resp.StatusCode, respBody)
	}

	if len(respBody) == 0 {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// response may be non-JSON (e.g. plain text OK) — return nil
		return nil
	}
	return result
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
