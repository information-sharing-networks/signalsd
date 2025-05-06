package helpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signals"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// RespondWithError logs the details of the error and writes a json response containing an error code and message
func RespondWithError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode signals.ErrorCode, message string) {
	reqLog, ok := r.Context().Value(signals.RequestLoggerKey).(*zerolog.Logger)
	if !ok {
		reqLog = &log.Logger
	}
	//reqLog := logger.FromContext(r.Context())
	reqID := middleware.GetReqID(r.Context())

	reqLog.Error().
		Int("status", statusCode).
		Any("error_code", errorCode).
		Str("error_message", message).
		Str("request_id", reqID).
		Msg("Error response")

	errResponse := signals.ErrorResponse{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		ReqID:      reqID,
	}

	dat, err := json.Marshal(errResponse)
	if err != nil {
		reqLog.Error().
			Err(err).
			Str("request_id", reqID).
			Msg("error marshaling error response")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"code":"internal_error","message":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write(dat)
}

func RespondWithJSON(w http.ResponseWriter, status int, payload any) {

	dat, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("error: could not decode the response payload")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(dat)
}

// GenerateSlug generates a URL-friendly slug from a given string.
func GenerateSlug(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("no input string supplied to GenerateSlug")
	}

	normalized := norm.NFD.String(input)

	withoutDiacritics, _, err := transform.String(runes.Remove(runes.In(unicode.Mn)), normalized)
	if err != nil {
		return "", fmt.Errorf("error creating slug: %v", err)
	}

	lowerCase := strings.ToLower(withoutDiacritics)

	reg := regexp.MustCompile(`[^a-z0-9\- ]+`) // Include space in the character set to handle it separately
	hyphenated := reg.ReplaceAllString(lowerCase, "-")

	spaceReg := regexp.MustCompile(`[ ]+`)
	hyphenated = spaceReg.ReplaceAllString(hyphenated, "-")

	trimmed := strings.Trim(hyphenated, "-")

	return trimmed, nil
}
func ValidateURL(rawURL string) error {
	_, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}
	return nil
}
