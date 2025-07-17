package utils

import (
	"testing"
)

func TestValidateGithubFileURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		fileType string
		wantErr  bool
	}{
		// Valid URLs
		{
			name:     "valid schema URL",
			url:      "https://github.com/owner/repo/blob/main/schema.json",
			fileType: "schema",
			wantErr:  false,
		},
		{
			name:     "valid readme URL",
			url:      "https://github.com/owner/repo/blob/main/readme.md",
			fileType: "readme",
			wantErr:  false,
		},
		{
			name:     "skip validation URL",
			url:      "https://github.com/skip/validation/main/schema.json",
			fileType: "schema",
			wantErr:  false,
		},
		// Invalid URLs - SSRF protection
		{
			name:     "non-HTTPS URL",
			url:      "http://github.com/owner/repo/blob/main/schema.json",
			fileType: "schema",
			wantErr:  true,
		},
		{
			name:     "non-GitHub domain",
			url:      "https://evil.com/owner/repo/blob/main/schema.json",
			fileType: "schema",
			wantErr:  true,
		},
		// Invalid file extensions
		{
			name:     "wrong extension for schema",
			url:      "https://github.com/owner/repo/blob/main/schema.txt",
			fileType: "schema",
			wantErr:  true,
		},
		{
			name:     "wrong extension for readme",
			url:      "https://github.com/owner/repo/blob/main/readme.txt",
			fileType: "readme",
			wantErr:  true,
		},
		// Invalid GitHub structure
		{
			name:     "missing repo name",
			url:      "https://github.com/owner/schema.json",
			fileType: "schema",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGithubFileURL(tt.url, tt.fileType)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGithubFileURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFetchGithubFileContent_SkipValidation(t *testing.T) {
	// Test the skip validation URL returns empty JSON
	content, err := FetchGithubFileContent("https://github.com/skip/validation/main/schema.json")
	if err != nil {
		t.Errorf("FetchGithubFileContent() error = %v, want nil", err)
	}
	if content != "{}" {
		t.Errorf("FetchGithubFileContent() content = %v, want {}", content)
	}
}
