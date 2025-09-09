package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// UpdateIsnAccountAccess grants or revokes an permissions to access an ISN
// accountType should be "user" or "service_account"
func (c *Client) UpdateIsnAccountAccess(accessToken, isnSlug, accountType, accountIdentifier, permission string) error {
	var accountID string

	// Lookup account based on type
	switch accountType {
	case "user":
		user, err := c.LookupUserByEmail(accessToken, accountIdentifier)
		if err != nil {
			return err
		}
		accountID = user.AccountID
	case "service_account":
		serviceAccount, err := c.LookupServiceAccountByClientID(accessToken, accountIdentifier)
		if err != nil {
			return err
		}
		accountID = serviceAccount.AccountID
	default:
		return NewClientInternalError(fmt.Errorf("invalid account type: %s", accountType), "validating account type")
	}

	url := fmt.Sprintf("%s/api/isn/%s/accounts/%s", c.baseURL, isnSlug, accountID)

	// Revoke access
	if permission == "none" {
		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			return NewClientInternalError(err, "revoke isn account access")
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

		res, err := c.httpClient.Do(req)
		if err != nil {
			return NewClientConnectionError(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			return NewClientApiError(res)
		}

		return nil
	}

	// Grant Access
	requestBody := map[string]string{
		"permission": permission,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return NewClientInternalError(err, "marshaling add account request")
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating add account request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

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
