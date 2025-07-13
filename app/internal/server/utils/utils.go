package utils

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// GenerateSlug generates a URL-friendly slug from a given string.
// slugs identify a set of versioned signal_types describing the same data set.
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

// ValidateGithubFileURL validates the GitHub URLs for schema and readme files submitted as part of the signal type definition
func ValidateGithubFileURL(rawURL string, fileType string) error {
	// Handle special skip validation URL for schemas
	if fileType == "schema" && rawURL == "https://github.com/skip/validation/main/schema.json" {
		return nil
	}

	// Example: https://github.com/user/repo/blob/branch/path/to/file.json
	pattern := `^https://github\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+/.+$`
	matched, err := regexp.MatchString(pattern, rawURL)
	if err != nil {
		return fmt.Errorf("internal error validating URL pattern: %w", err)
	}
	if !matched {
		return fmt.Errorf("URL must be a valid GitHub file URL (e.g., https://github.com/user/repo/blob/main/file)")
	}

	switch fileType {
	case "schema":
		if !strings.HasSuffix(rawURL, ".json") {
			return fmt.Errorf("URL must point to a .json file")
		}
	case "readme":
		if !strings.HasSuffix(rawURL, ".md") {
			return fmt.Errorf("URL must point to a .md file")
		}
	default:
		return fmt.Errorf("internal error: unsupported file type '%s' for validation", fileType)
	}

	return nil
}

// FetchGithubFileContent fetches content from GitHub URLs specified in the schema and readme fields of signal types
func FetchGithubFileContent(rawURL string) (string, error) {
	// Handle special skip validation URL
	if rawURL == "https://github.com/skip/validation/main/schema.json" {
		return "{}", nil
	}

	// Convert GitHub blob URLs to raw URLs
	// Example: https://github.com/org/repo/blob/main/file.json -> https://raw.githubusercontent.com/org/repo/main/file.json
	if strings.HasPrefix(rawURL, "https://github.com/") {
		rawURL = strings.Replace(rawURL, "https://github.com/", "https://raw.githubusercontent.com/", 1)
		rawURL = strings.Replace(rawURL, "/blob/", "/", 1)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Disable redirects for SSRF protection
		},
	}

	// #nosec G107 -- avoid false postive security linter warning - URL is validated to be GitHub-only before this function is called (see ValidateGithubFileURL)
	res, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch content: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch failed with status: %d", res.StatusCode)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(bodyBytes), nil
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

// check that the supplied string conforms to the date formats supported by the API (ISO 8601 or YYYY-MM-DD)
// Assume YYYY-MM-DD is the start of day in UTC e.g 2006-01-02T00:00:00Z
func ParseDateTime(dateString string) (time.Time, error) {
	isoLayouts := []string{
		time.RFC3339,     // e.g 2006-01-02T15:04:05Z07:00
		time.RFC3339Nano, // e.g 2006-01-02T15:04:05.999999999Z07:00
	}

	for _, layout := range isoLayouts {
		if t, err := time.Parse(layout, dateString); err == nil {
			return t, nil
		}
	}

	//If not an ISO 8601 with timezone, try YYYY.MM.DD
	if t, err := time.Parse("2006-01-02", dateString); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("unsupported date format: %s. Expected ISO 8601 with timezone (e.g., 2006-01-02T15:04:05+07:00) or YYYY-MM-DD (e.g., 2006-01-02)", dateString)
}

func GenerateClientID(organization string) (string, error) {
	organizationSlug, err := GenerateSlug(organization)
	if err != nil {
		return "", err
	}

	randomBytes := make([]byte, 6)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	randomSuffix := base32.StdEncoding.EncodeToString(randomBytes)[:8]

	return fmt.Sprintf("sa_%s_%s", strings.ToLower(organizationSlug), strings.ToLower(randomSuffix)), nil
}
