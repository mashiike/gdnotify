package gdnotify

import (
	"context"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/mashiike/gdnotify/pkg/gdnotifyevent"
	"google.golang.org/api/drive/v3"
)

// S3Copier copies files from Google Drive to S3 based on configuration rules.
type S3Copier struct {
	config     *S3CopyConfig
	env        *CELEnv
	downloader *DriveDownloader
	uploader   *S3Uploader
}

// NewS3Copier creates a new S3Copier.
func NewS3Copier(config *S3CopyConfig, env *CELEnv, driveSvc *drive.Service, awsCfg aws.Config) *S3Copier {
	return &S3Copier{
		config:     config,
		env:        env,
		downloader: NewDriveDownloader(driveSvc),
		uploader:   NewS3Uploader(awsCfg),
	}
}

// CopyResult contains the result of a copy operation.
type CopyResult struct {
	S3URI       string
	ContentType string
	Size        int64
	CopiedAt    time.Time
}

// Copy evaluates the configuration rules and copies the file to S3 if a rule matches.
// Returns nil if no rule matches or if the matched rule has skip=true.
// Removed files (change.removed=true) are always skipped.
// Errors are logged as warnings and do not stop execution.
func (c *S3Copier) Copy(ctx context.Context, detail *gdnotifyevent.Detail) *CopyResult {
	// Skip removed files by default - nothing to download
	if detail.Change != nil && detail.Change.Removed {
		slog.DebugContext(ctx, "s3copy: skipping removed file")
		return nil
	}

	rule, err := c.config.Match(c.env, detail)
	if err != nil {
		slog.WarnContext(ctx, "s3copy: failed to match rules", "error", err)
		return nil
	}
	if rule == nil {
		slog.DebugContext(ctx, "s3copy: no matching rule")
		return nil
	}
	if rule.Skip {
		slog.DebugContext(ctx, "s3copy: matched skip rule")
		return nil
	}

	bucketName, err := c.config.GetBucketName(c.env, rule, detail)
	if err != nil {
		slog.WarnContext(ctx, "s3copy: failed to evaluate bucket_name", "error", err)
		return nil
	}
	objectKey, err := c.config.GetObjectKey(c.env, rule, detail)
	if err != nil {
		slog.WarnContext(ctx, "s3copy: failed to evaluate object_key", "error", err)
		return nil
	}

	fileID := c.getFileID(detail)
	if fileID == "" {
		slog.WarnContext(ctx, "s3copy: no file ID found in detail")
		return nil
	}

	fileMimeType := c.getFileMimeType(detail)
	exportFormat := rule.Export

	slog.InfoContext(ctx, "s3copy: starting copy",
		"fileId", fileID,
		"bucket", bucketName,
		"key", objectKey,
		"export", exportFormat,
	)

	downloadResult, err := c.downloader.DownloadOrExport(ctx, fileID, fileMimeType, exportFormat)
	if err != nil {
		slog.WarnContext(ctx, "s3copy: failed to download/export file",
			"fileId", fileID,
			"error", err,
		)
		return nil
	}
	defer downloadResult.Body.Close()

	uploadOutput, err := c.uploader.Upload(ctx, &UploadInput{
		Bucket:      bucketName,
		Key:         objectKey,
		Body:        downloadResult.Body,
		ContentType: downloadResult.ContentType,
	})
	if err != nil {
		slog.WarnContext(ctx, "s3copy: failed to upload to S3",
			"bucket", bucketName,
			"key", objectKey,
			"error", err,
		)
		return nil
	}

	slog.InfoContext(ctx, "s3copy: copy completed",
		"s3Uri", uploadOutput.S3URI,
		"contentType", downloadResult.ContentType,
	)

	return &CopyResult{
		S3URI:       uploadOutput.S3URI,
		ContentType: downloadResult.ContentType,
		Size:        downloadResult.Size,
		CopiedAt:    time.Now(),
	}
}

func (c *S3Copier) getFileID(detail *gdnotifyevent.Detail) string {
	if detail.Change != nil && detail.Change.FileID != "" {
		return detail.Change.FileID
	}
	if detail.Entity != nil && detail.Entity.ID != "" {
		return detail.Entity.ID
	}
	return ""
}

func (c *S3Copier) getFileMimeType(detail *gdnotifyevent.Detail) string {
	if detail.Change != nil && detail.Change.File != nil {
		return detail.Change.File.MimeType
	}
	return ""
}
