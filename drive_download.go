package gdnotify

import (
	"context"
	"fmt"
	"io"
	"strings"

	"google.golang.org/api/drive/v3"
)

// ExportMIMETypes maps export format names to MIME types.
// Used for exporting Google Workspace files to standard formats.
var ExportMIMETypes = map[string]string{
	"pdf":  "application/pdf",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"csv":  "text/csv",
	"txt":  "text/plain",
	"html": "text/html",
	"rtf":  "application/rtf",
	"odt":  "application/vnd.oasis.opendocument.text",
	"ods":  "application/vnd.oasis.opendocument.spreadsheet",
	"odp":  "application/vnd.oasis.opendocument.presentation",
	"png":  "image/png",
	"jpeg": "image/jpeg",
	"svg":  "image/svg+xml",
}

// DriveDownloader handles file downloads and exports from Google Drive.
type DriveDownloader struct {
	svc *drive.Service
}

// NewDriveDownloader creates a new DriveDownloader with the given service.
func NewDriveDownloader(svc *drive.Service) *DriveDownloader {
	return &DriveDownloader{svc: svc}
}

// DownloadResult contains the result of a download or export operation.
type DownloadResult struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// Download downloads a regular file from Google Drive.
// Returns the file content, content type, and any error.
func (d *DriveDownloader) Download(ctx context.Context, fileID string) (*DownloadResult, error) {
	resp, err := d.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("download file %s: %w", fileID, err)
	}
	return &DownloadResult{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		Size:        resp.ContentLength,
	}, nil
}

// Export exports a Google Workspace file to the specified format.
// format should be one of: pdf, xlsx, docx, pptx, csv, txt, html, etc.
func (d *DriveDownloader) Export(ctx context.Context, fileID, format string) (*DownloadResult, error) {
	mimeType, ok := ExportMIMETypes[strings.ToLower(format)]
	if !ok {
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
	resp, err := d.svc.Files.Export(fileID, mimeType).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("export file %s as %s: %w", fileID, format, err)
	}
	return &DownloadResult{
		Body:        resp.Body,
		ContentType: mimeType,
		Size:        resp.ContentLength,
	}, nil
}

// IsGoogleWorkspaceFile returns true if the MIME type is a Google Workspace file.
func IsGoogleWorkspaceFile(mimeType string) bool {
	return strings.HasPrefix(mimeType, "application/vnd.google-apps.")
}

// DownloadOrExport downloads a file or exports it based on its MIME type.
// For Google Workspace files, exports to the specified format (default: pdf).
// For regular files, downloads the original.
func (d *DriveDownloader) DownloadOrExport(ctx context.Context, fileID, fileMimeType, exportFormat string) (*DownloadResult, error) {
	if IsGoogleWorkspaceFile(fileMimeType) {
		if exportFormat == "" {
			exportFormat = "pdf"
		}
		return d.Export(ctx, fileID, exportFormat)
	}
	return d.Download(ctx, fileID)
}
