package gdnotify

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsGoogleWorkspaceFile(t *testing.T) {
	tests := []struct {
		mimeType string
		expected bool
	}{
		{"application/vnd.google-apps.document", true},
		{"application/vnd.google-apps.spreadsheet", true},
		{"application/vnd.google-apps.presentation", true},
		{"application/vnd.google-apps.drawing", true},
		{"application/pdf", false},
		{"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", false},
		{"image/png", false},
		{"text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			result := IsGoogleWorkspaceFile(tt.mimeType)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestExportMIMETypes(t *testing.T) {
	tests := []struct {
		format   string
		expected string
		exists   bool
	}{
		{"pdf", "application/pdf", true},
		{"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true},
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", true},
		{"csv", "text/csv", true},
		{"txt", "text/plain", true},
		{"html", "text/html", true},
		{"png", "image/png", true},
		{"jpeg", "image/jpeg", true},
		{"svg", "image/svg+xml", true},
		{"unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			mimeType, ok := ExportMIMETypes[tt.format]
			require.Equal(t, tt.exists, ok)
			if ok {
				require.Equal(t, tt.expected, mimeType)
			}
		})
	}
}
