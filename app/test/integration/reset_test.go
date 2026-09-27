//go:build integration

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

// TestResetOnlyWorksInDevEnvironments checks POST /api/admin/reset (which deletes all the data in the database)
// is refused outside the dev environment - the test server runs as a "test" environment
func TestResetOnlyWorksInDevEnvironments(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	account := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@reset.com")

	response, err := http.Post(testEnv.baseURL+"/api/admin/reset", "", nil)
	if err != nil {
		t.Fatalf("Failed to make reset request: %v", err)
	}
	expectErrorCode(t, response, http.StatusForbidden, apperrors.ErrCodeForbidden)

	if _, err := testEnv.queries.GetAccountByID(ctx, account.ID); err != nil {
		t.Errorf("Expected the account to still exist after the refused reset: %v", err)
	}
}
