package types

import (
	"fmt"
	"time"
)

// FormatDateTime converts an RFC3339 datetime string to YYYY-MM-DD HH:MM
func FormatDateTime(dateString string) string {
	t, err := time.Parse(time.RFC3339, dateString)
	if err != nil {
		return dateString
	}

	return t.Format("2006-01-02 15:04")
}

// FormatFileSize converts a size in bytes to a readable size with the exact byte count, e.g. "470.8 KB (482133 bytes)"
func FormatFileSize(sizeBytes int64) string {
	const unit = 1024
	if sizeBytes < unit {
		return fmt.Sprintf("%d bytes", sizeBytes)
	}

	size := float64(sizeBytes)
	units := []string{"KB", "MB", "GB"}
	i := -1
	for size >= unit && i < len(units)-1 {
		size /= unit
		i++
	}

	return fmt.Sprintf("%.1f %s (%d bytes)", size, units[i], sizeBytes)
}

func FormatRecordsReturned(count int) string {
	if count == 1 {
		return "1 record"
	}
	return fmt.Sprintf("%d records", count)
}
