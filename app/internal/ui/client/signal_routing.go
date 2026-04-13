package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// SignalRoutingRule is the mapping between a pattern and a isn.
type SignalRoutingRule struct {
	MatchPattern     string `json:"match_pattern" example:"*felixstowe*"`
	Operator         string `json:"operator" enums:"matches,equals,does_not_match,does_not_equal" example:"matches"`
	IsCaseInsensitve bool   `json:"is_case_insensitive" example:"true"`
	IsnSlug          string `json:"isn_slug" example:"felixstowe-isn"`
	Sequence         int32  `json:"sequence" example:"1"`
}

// UpdateSignalRoutingConfigRequest is the body for setting routing rules.
type UpdateSignalRoutingConfigRequest struct {
	RoutingField string              `json:"routing_field" example:"payload.portOfEntry"`
	RoutingRules []SignalRoutingRule `json:"routing_rules"`
}

// SignalRoutingConfigResponse is the response from GET or PUT routing rules.
type SignalRoutingConfigResponse struct {
	SignalTypePath string              `json:"signal_type_path"`
	RoutingField   string              `json:"routing_field"`
	RoutingRules   []SignalRoutingRule `json:"routing_rules"`
}

// GetIsnRouting fetches the routing rule for a signal type version.
// Returns nil, nil when no routing rule exists (404).
func (c *Client) GetIsnRouting(accessToken, slug, semVer string) (*SignalRoutingConfigResponse, error) {
	url := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/routes", c.baseURL, slug, semVer)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating get ISN routing request")
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var detail SignalRoutingConfigResponse
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		return nil, NewClientInternalError(err, "decoding get ISN routing response")
	}
	return &detail, nil
}

// UpdateSignalRoutingConfig replaces the routing rule and all routes for a signal type version.
func (c *Client) UpdateSignalRoutingConfig(accessToken, slug, semVer string, req UpdateSignalRoutingConfigRequest) (*SignalRoutingConfigResponse, error) {
	url := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/routes", c.baseURL, slug, semVer)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, NewClientInternalError(err, "marshaling put ISN routing request")
	}

	httpReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, NewClientInternalError(err, "creating put ISN routing request")
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var detail SignalRoutingConfigResponse
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		return nil, NewClientInternalError(err, "decoding put ISN routing response")
	}
	return &detail, nil
}

// DeleteSignalRoutingConfig removes all routing rules for a signal type version.
func (c *Client) DeleteSignalRoutingConfig(accessToken, slug, semVer string) error {
	url := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/routes", c.baseURL, slug, semVer)

	httpReq, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return NewClientInternalError(err, "creating delete ISN routing request")
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

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
