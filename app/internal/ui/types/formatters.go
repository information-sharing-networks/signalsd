package types

import "time"

// FormatDateTime converts an RFC3339 datetime string to YYYY-MM-DD HH:MM
func FormatDateTime(dateString string) string {
	t, err := time.Parse(time.RFC3339, dateString)
	if err != nil {
		return dateString
	}

	return t.Format("2006-01-02 15:04")
}
