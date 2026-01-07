package gdnotify

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Uploader handles file uploads to Amazon S3.
type S3Uploader struct {
	client *s3.Client
}

// NewS3Uploader creates a new S3Uploader with the given AWS config.
func NewS3Uploader(cfg aws.Config) *S3Uploader {
	return &S3Uploader{
		client: s3.NewFromConfig(cfg),
	}
}

// UploadInput contains parameters for uploading a file to S3.
type UploadInput struct {
	Bucket      string
	Key         string
	Body        io.Reader
	ContentType string
}

// UploadOutput contains the result of an upload operation.
type UploadOutput struct {
	S3URI string
	Size  int64
}

// Upload uploads data to S3 and returns the S3 URI.
func (u *S3Uploader) Upload(ctx context.Context, input *UploadInput) (*UploadOutput, error) {
	// Read body into buffer to get Content-Length
	buf, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, fmt.Errorf("read body for s3://%s/%s: %w", input.Bucket, input.Key, err)
	}

	putInput := &s3.PutObjectInput{
		Bucket:        aws.String(input.Bucket),
		Key:           aws.String(input.Key),
		Body:          bytes.NewReader(buf),
		ContentLength: aws.Int64(int64(len(buf))),
	}
	if input.ContentType != "" {
		putInput.ContentType = aws.String(input.ContentType)
	}

	_, err = u.client.PutObject(ctx, putInput)
	if err != nil {
		return nil, fmt.Errorf("upload to s3://%s/%s: %w", input.Bucket, input.Key, err)
	}

	return &UploadOutput{
		S3URI: fmt.Sprintf("s3://%s/%s", input.Bucket, input.Key),
		Size:  int64(len(buf)),
	}, nil
}
