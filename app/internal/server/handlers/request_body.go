package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

// decodeJSONBody decodes the JSON request body into target (a pointer to the request struct, e.g. &req).
//
// It returns 413 request_too_large if the body is larger than the limit set by the RequestSizeLimit middleware
// (requests without a Content-Length only reach the limit when the body is read),
// otherwise 400 malformed_body if the body can't be decoded.
func decodeJSONBody(r *http.Request, target any) error {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return apperrors.RequestTooLarge(maxBytesErr.Limit)
		}
		return apperrors.MalformedBody("invalid JSON body", err)
	}
	return nil
}

// decodeJSONBodyStrict is the same as decodeJSONBody, but also rejects bodies that contain fields target does not have.
func decodeJSONBodyStrict(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return apperrors.RequestTooLarge(maxBytesErr.Limit)
		}
		return apperrors.MalformedBody("invalid JSON body", err)
	}
	return nil
}
