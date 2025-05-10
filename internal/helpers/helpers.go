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

// currently links to files in files in public github repos are supported - it is recommended to link to a tagged version of the file,
// e.g https://github.com/nickabs/transmission/blob/v2.21.2/locales/af.json
func CheckSignalDefURL(rawURL string, urlType string) error {
	// TODO - additional checks, e.g checking the file exists and - in case of scheam urls - is a valid json schema.
	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL supplied: %w", err)
	}

	if parsedURL.Scheme != "https" || parsedURL.Host != "github.com" {
		return fmt.Errorf("expecting an absolute url for a file in a public github repo, e.g https://github.com/user/project/v0.0.1/locales/filename")
	}

	suffixPattern := `\.(([^\.\/]+$))`
	re := regexp.MustCompile(suffixPattern)
	suffix := re.FindStringSubmatch(parsedURL.Path)[0]

	switch urlType {
	case "schema":
		if suffix != ".json" {
			return fmt.Errorf("expected a .json file")
		}
	case "readme":
		if suffix != ".md" {
			return fmt.Errorf("expected a .md file")
		}
	default:
		return fmt.Errorf("internal server error - invalid url type sent to function %s", urlType)
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

func GetScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}

	// Check common reverse proxy headers
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}

// check for valid origins, e.g http://localhost:8080 , https://example.com etc
func IsValidOrigin(urlStr string) bool {
	re := regexp.MustCompile(`^(https?):\/\/([a-zA-Z0-9_\-\.]+)(:\d+)?$`)
	return re.MatchString(urlStr)
}
