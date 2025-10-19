package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// TransferIsnOwnershipRequest represents the request to transfer ISN ownership
type TransferIsnOwnershipRequest struct {
	NewOwnerAccountID string `json:"new_owner_account_id"`
}

// UpdateIsnAccounts grants or revokes an permissions to access an ISN
func (c *Client) UpdateIsnAccounts(accessToken, isnSlug, accountType, accountIdentifier, permission string) error {
	var accountID string

	switch accountType {
	case "user":
		user, err := c.LookupUserByEmail(accessToken, accountIdentifier)
		if err != nil {
			return err
		}
		accountID = user.AccountID
	case "service-account":
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

// TransferIsnOwnership transfers ownership of an ISN to another admin account
func (c *Client) TransferIsnOwnership(accessToken, isnSlug, newOwnerEmail string) error {
	// First, lookup the new owner by email to get their account ID
	user, err := c.LookupUserByEmail(accessToken, newOwnerEmail)
	if err != nil {
		return err
	}

	req := TransferIsnOwnershipRequest{
		NewOwnerAccountID: user.AccountID,
	}

	url := fmt.Sprintf("%s/api/admin/isn/%s/transfer-ownership", c.baseURL, isnSlug)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return NewClientInternalError(err, "marshaling transfer ownership request")
	}

	httpReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating transfer ownership request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	httpReq.Header.Set("Content-Type", "application/json")

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
