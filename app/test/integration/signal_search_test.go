//go:build integration

package integration

// Signal search tests:
//   GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search         (private ISNs)
//   GET /api/public/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search  (public ISNs)
//
// - TestSignalSearchAccess: who can search which ISNs
// - TestWithdrawnSignalSearch: withdrawn signals are only returned with include_withdrawn=true
// - TestWriteOnlyAccountVisibility: write-only accounts only see their own signals (see TestCorrelationSearch for correlated signals)
// - TestPreviousVersionsSearch: include_previous_versions
// - TestCorrelationSearch: include_correlated and the correlation_id filter

import (
	"context"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// TestSignalSearchAccess checks which accounts can search which ISNs:
// - public ISNs can be searched without authentication (and do not show email addresses)
// - private ISNs can't be searched via the public endpoint
// - private ISNs can only be searched by accounts with access to the ISN
func TestSignalSearchAccess(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	// two private ISNs with one signal each:
	// - the siteadmin can search both ISNs
	// - the admin owns the admin ISN and can't search the siteadmin ISN
	// - the member has read access to the admin ISN and can't search the siteadmin ISN
	//
	// and a public ISN with one signal, that anyone can search
	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@search.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@search.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@search.com")

	siteAdminISN := createTestISN(t, ctx, testEnv.queries, "siteadmin-search-isn", "SiteAdmin search ISN", siteAdminAccount.ID, "private")
	adminISN := createTestISN(t, ctx, testEnv.queries, "admin-search-isn", "Admin search ISN", adminAccount.ID, "private")
	publicISN := createTestISN(t, ctx, testEnv.queries, "public-search-isn", "Public search ISN", adminAccount.ID, "public")

	siteAdminSignalType := createTestSignalType(t, ctx, testEnv.queries, siteAdminISN.ID, "siteadmin ISN search signal", "", signalsd.ContentKindJSON)
	adminSignalType := createTestSignalType(t, ctx, testEnv.queries, adminISN.ID, "admin ISN search signal", "", signalsd.ContentKindJSON)
	publicSignalType := createTestSignalType(t, ctx, testEnv.queries, publicISN.ID, "public ISN search signal", "", signalsd.ContentKindJSON)

	grantPermission(t, ctx, testEnv.queries, adminISN.ID, memberAccount.ID, "read")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}
	if err := testEnv.publicIsnCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh public ISN cache: %v", err)
	}

	// tokens are created after the permissions are granted so the claims include the ISN permissions
	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)
	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	memberToken := testEnv.getAccessToken(t, memberAccount.ID)

	siteAdminEndpoint := newTestSignalEndpoint(siteAdminISN, siteAdminSignalType)
	adminEndpoint := newTestSignalEndpoint(adminISN, adminSignalType)
	publicEndpoint := newTestSignalEndpoint(publicISN, publicSignalType)

	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("siteadmin-search-001"), siteAdminToken, siteAdminEndpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("admin-search-001"), adminToken, adminEndpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("public-search-001"), adminToken, publicEndpoint)

	t.Run("public ISN can be searched without authentication", func(t *testing.T) {
		signals := expectSearchResults(t, searchPublicSignals(t, testEnv.baseURL, publicEndpoint, lastHourSearchParams()))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal, got %d", len(signals))
		}
		if email, ok := signals[0]["email"].(string); ok && email != "" {
			t.Errorf("Found email %s in a public ISN search - emails must not be shown in public ISNs", email)
		}
	})

	t.Run("private ISN can't be searched via the public endpoint", func(t *testing.T) {
		response := searchPublicSignals(t, testEnv.baseURL, siteAdminEndpoint, lastHourSearchParams())
		expectStatus(t, response, http.StatusNotFound)
	})

	tests := []struct {
		name            string
		token           string
		endpoint        testSignalEndpoint
		expectedStatus  int
		expectedSignals int // only checked when the search succeeds
	}{
		{
			name:            "siteadmin can search their own ISN",
			token:           siteAdminToken,
			endpoint:        siteAdminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
		},
		{
			name:            "siteadmin can search other ISNs",
			token:           siteAdminToken,
			endpoint:        adminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
		},
		{
			name:            "ISN owner can search their own ISN",
			token:           adminToken,
			endpoint:        adminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
		},
		{
			name:           "ISN owner can't search another owner's ISN",
			token:          adminToken,
			endpoint:       siteAdminEndpoint,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:            "member with read access can search the ISN",
			token:           memberToken,
			endpoint:        adminEndpoint,
			expectedStatus:  http.StatusOK,
			expectedSignals: 1,
		},
		{
			name:           "member can't search an ISN they don't have access to",
			token:          memberToken,
			endpoint:       siteAdminEndpoint,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "expired access token is rejected",
			token:          createExpiredAccessToken(t, adminAccount.ID, testEnv.cfg.SecretKey),
			endpoint:       adminEndpoint,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := searchPrivateSignals(t, testEnv.baseURL, tt.endpoint, tt.token, lastHourSearchParams())

			if tt.expectedStatus == http.StatusOK {
				signals := expectSearchResults(t, response)
				if len(signals) != tt.expectedSignals {
					t.Errorf("Expected %d signals, got %d", tt.expectedSignals, len(signals))
				}
				return
			}

			// error responses must only contain the standard error fields (no data leakage)
			errorResponse := expectJSONResponse(t, response, tt.expectedStatus)
			if _, ok := errorResponse["error_code"]; !ok {
				t.Errorf("Error response is missing the error_code field: %v", errorResponse)
			}
			if _, ok := errorResponse["message"]; !ok {
				t.Errorf("Error response is missing the message field: %v", errorResponse)
			}
			for field := range errorResponse {
				if field != "error_code" && field != "message" && field != "request_id" {
					t.Errorf("Unexpected field %q in error response - potential data leakage: %v", field, errorResponse)
				}
			}
			if tt.expectedStatus == http.StatusForbidden && errorResponse["error_code"] != apperrors.ErrCodeForbidden.String() {
				t.Errorf("Expected error_code %s, got %v", apperrors.ErrCodeForbidden, errorResponse["error_code"])
			}
		})
	}
}

// TestWithdrawnSignalSearch checks withdrawn signals are only returned when include_withdrawn=true
func TestWithdrawnSignalSearch(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@withdrawn.com")
	isn := createTestISN(t, ctx, testEnv.queries, "withdrawn-isn", "Withdrawn ISN", adminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "withdrawn signal", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	endpoint := newTestSignalEndpoint(isn, signalType)

	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("withdrawn-001"), adminToken, endpoint)

	signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, adminToken, lastHourSearchParams()))
	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal before withdrawal, got %d", len(signals))
	}

	expectStatus(t, withdrawSignal(t, testEnv.baseURL, endpoint, adminToken, "withdrawn-001"), http.StatusNoContent)

	t.Run("withdrawn signals are excluded by default", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, adminToken, lastHourSearchParams()))
		if len(signals) != 0 {
			t.Errorf("Expected 0 signals, got %d", len(signals))
		}
	})

	t.Run("withdrawn signals are included with include_withdrawn=true", func(t *testing.T) {
		params := lastHourSearchParams()
		params["include_withdrawn"] = "true"
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, adminToken, params))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal, got %d", len(signals))
		}
		if signals[0]["is_withdrawn"] != true {
			t.Errorf("Expected is_withdrawn=true, got %v", signals[0]["is_withdrawn"])
		}
	})
}

// TestWriteOnlyAccountVisibility checks that write-only accounts can only see the signals they created
// (when no signals are correlated to them)
func TestWriteOnlyAccountVisibility(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	account1 := createTestAccount(t, ctx, testEnv.queries, "member", "user", "account1@writeonly.com")
	account2 := createTestAccount(t, ctx, testEnv.queries, "member", "user", "account2@writeonly.com")

	isn := createTestISN(t, ctx, testEnv.queries, "shared-writeonly-isn", "Shared Write-Only ISN", account1.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "shared signal type", "", signalsd.ContentKindJSON)

	grantPermission(t, ctx, testEnv.queries, isn.ID, account1.ID, "write")
	grantPermission(t, ctx, testEnv.queries, isn.ID, account2.ID, "write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	token1 := testEnv.getAccessToken(t, account1.ID)
	token2 := testEnv.getAccessToken(t, account2.ID)

	endpoint := newTestSignalEndpoint(isn, signalType)

	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("account1-signal-001"), token1, endpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("account2-signal-001"), token2, endpoint)

	tests := []struct {
		name             string
		token            string
		params           map[string]string
		expectedLocalRef string // empty if no signals are expected
	}{
		{
			name:             "account 1 only sees its own signal",
			token:            token1,
			params:           lastHourSearchParams(),
			expectedLocalRef: "account1-signal-001",
		},
		{
			name:             "account 2 only sees its own signal",
			token:            token2,
			params:           lastHourSearchParams(),
			expectedLocalRef: "account2-signal-001",
		},
		{
			name:   "filtering on another account_id returns no signals (none are correlated to account 1's signals)",
			token:  token1,
			params: map[string]string{"account_id": account2.ID.String()},
		},
		{
			name:             "filtering on its own account_id returns its signal",
			token:            token1,
			params:           map[string]string{"account_id": account1.ID.String()},
			expectedLocalRef: "account1-signal-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, tt.token, tt.params))

			if tt.expectedLocalRef == "" {
				if len(signals) != 0 {
					t.Errorf("Expected 0 signals, got %d", len(signals))
				}
				return
			}
			if len(signals) != 1 || signals[0]["local_ref"] != tt.expectedLocalRef {
				t.Errorf("Expected only %s, got %v", tt.expectedLocalRef, signals)
			}
		})
	}
}

// TestPreviousVersionsSearch checks the previous versions of a signal are returned with include_previous_versions=true
func TestPreviousVersionsSearch(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@previous-versions.com")
	isn := createTestISN(t, ctx, testEnv.queries, "previous-versions-isn", "Previous versions ISN", adminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "versioned signal", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	endpoint := newTestSignalEndpoint(isn, signalType)

	// resubmitting a local_ref creates a new version of the signal
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("two-versions-001"), adminToken, endpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("two-versions-001"), adminToken, endpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("one-version-001"), adminToken, endpoint)

	t.Run("previous versions are not returned by default", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, adminToken, lastHourSearchParams()))
		for _, signal := range signals {
			if signal["previous_signal_versions"] != nil {
				t.Errorf("Expected no previous versions for %v", signal["local_ref"])
			}
		}
	})

	t.Run("previous versions are returned with include_previous_versions=true", func(t *testing.T) {
		params := lastHourSearchParams()
		params["include_previous_versions"] = "true"
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, adminToken, params))
		if len(signals) != 2 {
			t.Fatalf("Expected 2 signals, got %d", len(signals))
		}

		twoVersions := findSignalByLocalRef(signals, "two-versions-001")
		if twoVersions == nil {
			t.Fatal("two-versions-001 not found in search results")
		}
		if twoVersions["version_number"] != float64(2) {
			t.Errorf("Expected the latest version (2) to be returned, got %v", twoVersions["version_number"])
		}
		previousVersions, _ := twoVersions["previous_signal_versions"].([]any)
		if len(previousVersions) != 1 {
			t.Errorf("Expected 1 previous version for two-versions-001, got %d", len(previousVersions))
		}

		oneVersion := findSignalByLocalRef(signals, "one-version-001")
		if oneVersion == nil {
			t.Fatal("one-version-001 not found in search results")
		}
		if oneVersion["previous_signal_versions"] != nil {
			t.Errorf("Expected no previous versions for one-version-001, got %v", oneVersion["previous_signal_versions"])
		}
	})
}

// TestCorrelationSearch checks searches for signals linked to a master signal:
// - include_correlated embeds the signals linked to each returned signal, whatever their signal type
// - the correlation_id filter returns the signals of the searched type that are linked to a signal
// - write-only accounts see all the signals correlated to their own signals, and only their own signals correlated to other accounts' signals
// - email addresses of correlated signals are not shown in public ISNs
func TestCorrelationSearch(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@correlation-search.com")
	writer1Account := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer1@correlation-search.com")
	writer2Account := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer2@correlation-search.com")
	readerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "reader@correlation-search.com")

	privateISN := createTestISN(t, ctx, testEnv.queries, "correlation-search-isn", "Correlation search ISN", adminAccount.ID, "private")
	consignmentType := createTestSignalType(t, ctx, testEnv.queries, privateISN.ID, "consignment", "", signalsd.ContentKindJSON)
	billOfLadingType := createTestSignalType(t, ctx, testEnv.queries, privateISN.ID, "bill of lading", "", signalsd.ContentKindJSON)

	grantPermission(t, ctx, testEnv.queries, privateISN.ID, writer1Account.ID, "write")
	grantPermission(t, ctx, testEnv.queries, privateISN.ID, writer2Account.ID, "write")
	grantPermission(t, ctx, testEnv.queries, privateISN.ID, readerAccount.ID, "read")

	publicISN := createTestISN(t, ctx, testEnv.queries, "correlation-search-public-isn", "Correlation search public ISN", adminAccount.ID, "public")
	publicSignalType := createTestSignalType(t, ctx, testEnv.queries, publicISN.ID, "public correlation signal", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}
	if err := testEnv.publicIsnCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh public ISN cache: %v", err)
	}

	adminToken := testEnv.getAccessToken(t, adminAccount.ID)
	writer1Token := testEnv.getAccessToken(t, writer1Account.ID)
	writer2Token := testEnv.getAccessToken(t, writer2Account.ID)
	readerToken := testEnv.getAccessToken(t, readerAccount.ID)

	consignmentEndpoint := newTestSignalEndpoint(privateISN, consignmentType)
	billOfLadingEndpoint := newTestSignalEndpoint(privateISN, billOfLadingType)
	publicEndpoint := newTestSignalEndpoint(publicISN, publicSignalType)

	// writer 1 creates a consignment, and both writers link a bill of lading to it
	consignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-001"), writer1Token, consignmentEndpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithCorrelatedID("bol-writer1", consignmentID), writer1Token, billOfLadingEndpoint)
	bolWriter2ID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithCorrelatedID("bol-writer2", consignmentID), writer2Token, billOfLadingEndpoint)

	// writer 2 links a signal to its own bill of lading (writer 1 can't see it: correlation is one level deep)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithCorrelatedID("bol-writer2-annex", bolWriter2ID), writer2Token, billOfLadingEndpoint)

	// a consignment and a bill of lading with nothing linked to them
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-unlinked"), writer1Token, consignmentEndpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("bol-unlinked"), writer1Token, billOfLadingEndpoint)

	// public ISN: master signal with a correlated signal
	publicMasterID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("public-master"), adminToken, publicEndpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithCorrelatedID("public-linked", publicMasterID), adminToken, publicEndpoint)

	t.Run("correlated signals are not returned by default", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, consignmentEndpoint, readerToken, lastHourSearchParams()))
		for _, signal := range signals {
			if signal["correlated_signals"] != nil {
				t.Errorf("Expected no correlated signals for %v", signal["local_ref"])
			}
		}
	})

	t.Run("include_correlated only adds correlated signals to linked signals", func(t *testing.T) {
		params := lastHourSearchParams()
		params["include_correlated"] = "true"
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, consignmentEndpoint, readerToken, params))
		if len(signals) != 2 {
			t.Fatalf("Expected 2 consignments, got %d", len(signals))
		}

		linked, _ := findSignalByLocalRef(signals, "consignment-001")["correlated_signals"].([]any)
		if len(linked) != 2 {
			t.Errorf("Expected 2 correlated signals for consignment-001, got %d", len(linked))
		}
		if unlinked := findSignalByLocalRef(signals, "consignment-unlinked"); unlinked["correlated_signals"] != nil {
			t.Errorf("Expected no correlated signals for consignment-unlinked, got %v", unlinked["correlated_signals"])
		}
	})

	t.Run("correlated signals include their signal type", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, consignmentEndpoint, readerToken, map[string]string{
			"signal_id":          consignmentID,
			"include_correlated": "true",
		}))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal, got %d", len(signals))
		}

		expectedFields := map[string]any{
			"signal_type_slug": consignmentType.Slug,
			"sem_ver":          consignmentType.SemVer,
			"content_kind":     signalsd.ContentKindJSON,
			"account_type":     "user",
		}
		for field, expected := range expectedFields {
			if signals[0][field] != expected {
				t.Errorf("Expected %s %v, got %v", field, expected, signals[0][field])
			}
		}

		correlated, _ := signals[0]["correlated_signals"].([]any)
		if len(correlated) != 2 {
			t.Fatalf("Expected 2 correlated signals, got %d", len(correlated))
		}
		expectedFields["signal_type_slug"] = billOfLadingType.Slug
		expectedFields["sem_ver"] = billOfLadingType.SemVer
		for _, c := range correlated {
			signal := c.(map[string]any)
			for field, expected := range expectedFields {
				if signal[field] != expected {
					t.Errorf("Expected correlated signal %s %v, got %v", field, expected, signal[field])
				}
			}
		}
	})

	t.Run("correlation_id filter returns the linked signals", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{
			"correlation_id": consignmentID,
		}))
		if len(signals) != 2 || findSignalByLocalRef(signals, "bol-writer1") == nil || findSignalByLocalRef(signals, "bol-writer2") == nil {
			t.Errorf("Expected bol-writer1 and bol-writer2, got %v", signals)
		}
	})

	t.Run("correlation_id filter excludes the master signal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, consignmentEndpoint, readerToken, map[string]string{
			"correlation_id": consignmentID,
		}))
		if len(signals) != 0 {
			t.Errorf("Expected 0 signals, got %d", len(signals))
		}
	})

	t.Run("invalid correlation_id is rejected", func(t *testing.T) {
		response := searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{
			"correlation_id": "not-a-uuid",
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
	})

	t.Run("write-only account sees all the signals correlated to its own signal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, consignmentEndpoint, writer1Token, map[string]string{
			"signal_id":          consignmentID,
			"include_correlated": "true",
		}))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal, got %d", len(signals))
		}
		correlated, _ := signals[0]["correlated_signals"].([]any)
		if len(correlated) != 2 {
			t.Fatalf("Expected 2 correlated signals (bol-writer1 and bol-writer2), got %d", len(correlated))
		}
	})

	t.Run("write-only account correlation_id filter returns all the signals correlated to its own signal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, writer1Token, map[string]string{
			"correlation_id": consignmentID,
		}))
		if len(signals) != 2 || findSignalByLocalRef(signals, "bol-writer1") == nil || findSignalByLocalRef(signals, "bol-writer2") == nil {
			t.Errorf("Expected bol-writer1 and bol-writer2, got %v", signals)
		}
	})

	t.Run("write-only account account_id filter returns the other account's signals correlated to its own signals", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, writer1Token, map[string]string{
			"account_id": writer2Account.ID.String(),
		}))
		if len(signals) != 1 || signals[0]["local_ref"] != "bol-writer2" {
			t.Errorf("Expected only bol-writer2, got %v", signals)
		}
	})

	t.Run("write-only account can't see signals correlated to another account's signal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, writer1Token, map[string]string{
			"signal_id":          bolWriter2ID,
			"include_correlated": "true",
		}))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal (bol-writer2), got %d", len(signals))
		}
		if correlated := signals[0]["correlated_signals"]; correlated != nil {
			t.Errorf("Expected no correlated signals (bol-writer2-annex is correlated to writer 2's signal), got %v", correlated)
		}

		signals = expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, writer1Token, map[string]string{
			"correlation_id": bolWriter2ID,
		}))
		if len(signals) != 0 {
			t.Errorf("Expected 0 signals, got %v", signals)
		}
	})

	t.Run("write-only account only sees its own signals correlated to another account's signal", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, writer2Token, map[string]string{
			"correlation_id": consignmentID,
		}))
		if len(signals) != 1 || signals[0]["local_ref"] != "bol-writer2" {
			t.Errorf("Expected only bol-writer2, got %v", signals)
		}
	})

	t.Run("write-only account can't see the other account's signal its signal is correlated to", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, consignmentEndpoint, writer2Token, map[string]string{
			"signal_id": consignmentID,
		}))
		if len(signals) != 0 {
			t.Errorf("Expected 0 signals, got %v", signals)
		}
	})

	t.Run("public search does not show the email of correlated signals", func(t *testing.T) {
		signals := expectSearchResults(t, searchPublicSignals(t, testEnv.baseURL, publicEndpoint, map[string]string{
			"signal_id":          publicMasterID,
			"include_correlated": "true",
		}))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal, got %d", len(signals))
		}
		correlated, _ := signals[0]["correlated_signals"].([]any)
		if len(correlated) != 1 {
			t.Fatalf("Expected 1 correlated signal, got %d", len(correlated))
		}
		if email, ok := correlated[0].(map[string]any)["email"]; ok && email != "" {
			t.Errorf("Found email %v on a correlated signal in a public ISN search", email)
		}
	})
}
