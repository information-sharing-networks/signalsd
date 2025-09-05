package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// SearchSignals use the signalsd API to search for signals
func (c *Client) SearchSignals(accessToken string, params types.SignalSearchParams, visibility string) (*types.SignalSearchResponse, error) {
	// Build URL based on ISN visibility (public ISNs use /api/public/, private use /api/)
	var url string
	if visibility == "public" {
		url = fmt.Sprintf("%s/api/public/isn/%s/signal_types/%s/v%s/signals/search",
			c.baseURL, params.IsnSlug, params.SignalTypeSlug, params.SemVer)
	} else {
		url = fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals/search",
			c.baseURL, params.IsnSlug, params.SignalTypeSlug, params.SemVer)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating search request")
	}

	// Add query parameters
	q := req.URL.Query()
	if params.StartDate != "" {
		q.Add("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Add("end_date", params.EndDate)
	}
	if params.AccountID != "" {
		q.Add("account_id", params.AccountID)
	}
	if params.SignalID != "" {
		q.Add("signal_id", params.SignalID)
	}
	if params.LocalRef != "" {
		q.Add("local_ref", params.LocalRef)
	}
	if params.IncludeWithdrawn {
		q.Add("include_withdrawn", "true")
	}
	if params.IncludeCorrelated {
		q.Add("include_correlated", "true")
	}
	if params.IncludePreviousVersions {
		q.Add("include_previous_versions", "true")
	}
	req.URL.RawQuery = q.Encode()

	// Set authorization header for private ISNs (public ISNs don't need auth)
	if visibility == "private" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var searchResp types.SignalSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&searchResp); err != nil {
		return nil, NewClientInternalError(err, "decoding search response")
	}

	return &searchResp, nil
}
