package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// Login authenticates a user with the signalsd API
func (c *Client) Login(email, password string) (*types.AccessTokenDetails, *http.Cookie, error) {
	loginReq := auth.LoginRequest{
		Email:    email,
		Password: password,
	}

	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return nil, nil, NewClientInternalError(err, "marshaling login request")
	}

	url := fmt.Sprintf("%s/api/auth/login", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, NewClientInternalError(err, "creating login request")
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, nil, NewClientApiError(res)
	}

	var accessTokenDetails types.AccessTokenDetails
	if err := json.NewDecoder(res.Body).Decode(&accessTokenDetails); err != nil {
		return nil, nil, NewClientInternalError(err, "decoding access token response")
	}

	// Extract the refresh token cookie from the API response
	var refreshTokenCookie *http.Cookie
	for _, cookie := range res.Cookies() {
		if cookie.Name == signalsd.RefreshTokenCookieName {
			refreshTokenCookie = cookie
			break
		}
	}

	if refreshTokenCookie == nil {
		return nil, nil, NewClientInternalError(fmt.Errorf("cookie not found"), "extracting refresh token cookie from response")
	}

	return &accessTokenDetails, refreshTokenCookie, nil
}
