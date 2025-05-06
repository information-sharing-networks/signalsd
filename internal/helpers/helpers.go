package helpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
// slugs identify a set of versioned signal_defs describing the same data set.
// Only the owner of the inital slug can update it or add new versions.
// A slug can't be owned by more than one user.
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

// currently only github links are supported - it is recommended that user use linked to tagged versions of the file
// e.g https://github.com/nickabs/transmission/blob/v2.21.2/locales/af.json
// / TODO - other checks including checking the file exists and - in case of scheam urls - is a valid json schema.
func CheckUrl(rawURL string) error {
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL supplied: %w", err)
	}

	if parsedURL.Scheme != "https" || parsedURL.Host != "github.com" {
		return fmt.Errorf("link must be a https://github.com url (make sure to include both the scheme and hostname)")
	}

	return nil
}

// expects a semver in the form "major.minor.patch" and increments major/minor/patch according to supplied bump_type
func IncrementSemVer(bump_type string, semVer string) (string, error) {

	components := strings.Split(semVer, ".")

	if len(components) != 3 {
		return "", fmt.Errorf("can't bump version, invalid semVer supplied")
	}

	major, err := strconv.Atoi(components[0])
	if err != nil {
		return "", fmt.Errorf("can't bump version, invalid semVer supplied")
	}
	minor, err := strconv.Atoi(components[1])
	if err != nil {
		return "", fmt.Errorf("can't bump version, invalid semVer supplied")
	}
	patch, err := strconv.Atoi(components[2])
	if err != nil {
		return "", fmt.Errorf("can't bump version, invalid semVer supplied")
	}

	switch bump_type {
	case "major":
		major++
		minor = 0
		patch = 0
	case "minor":
		minor++
		patch = 0
	case "patch":
		patch++
	default:
		return "", fmt.Errorf("can't bump version, invalid bump type supplied")
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch), nil
}
