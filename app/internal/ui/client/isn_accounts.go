package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// AddAccountToIsn adds an account to an ISN with the specified permission
func (c *Client) AddAccountToIsn(accessToken, isnSlug, accountEmail, permission string) error {

	user, err := c.LookupUserByEmail(accessToken, accountEmail)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/isn/%s/accounts/%s", c.baseURL, isnSlug, user.AccountID)

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
