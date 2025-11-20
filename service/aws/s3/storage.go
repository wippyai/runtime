package s3

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wippyai/runtime/api/cloudstorage"
	services3 "github.com/wippyai/runtime/api/service/aws/s3"
	"go.uber.org/zap"
)

// Storage implements the cloudstorage.Storage interface for AWS S3
type Storage struct {
	// client is the AWS S3 client instance
	client *s3.Client

	// bucket is the default S3 bucket name
	bucket string

	// config holds the provider configuration
	config *services3.Config

	// log is the logger instance
	log *zap.Logger
}

// NewStorage creates a new Storage instance with the provided client and bucket
func NewStorage(client *s3.Client, bucket string, config *services3.Config, log *zap.Logger) *Storage {
	return &Storage{
		client: client,
		bucket: bucket,
		config: config,
		log:    log.With(zap.String("component", "s3storage"), zap.String("bucket", bucket)),
	}
}

// ListObjects lists objects in the S3 bucket with the given options
func (s *Storage) ListObjects(ctx context.Context, opts *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	// Initialize the S3 input parameters
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	}

	// Apply options if provided
	if opts != nil {
		if opts.Prefix != "" {
			input.Prefix = aws.String(opts.Prefix)
		}
		if opts.MaxKeys > 0 {
			//nolint:gosec // impossible to overflow
			input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
		}
		if opts.ContinuationToken != "" {
			input.ContinuationToken = aws.String(opts.ContinuationToken)
		}
	}

	// Call the AWS S3 API
	output, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		s.log.Error("list objects failed", zap.Error(err))
		return nil, err
	}

	// Prepare the result
	result := &cloudstorage.ListObjectsResult{
		IsTruncated:           aws.ToBool(output.IsTruncated),
		NextContinuationToken: aws.ToString(output.NextContinuationToken),
		Objects:               make([]cloudstorage.ObjectMetadata, 0, len(output.Contents)),
	}

	// Convert AWS objects to our ObjectMetadata format
	for _, item := range output.Contents {
		result.Objects = append(result.Objects, cloudstorage.ObjectMetadata{
			Key:  aws.ToString(item.Key),
			Size: aws.ToInt64(item.Size),
			ETag: aws.ToString(item.ETag),
			// ContentType is not available in ListObjectsV2 response
		})
	}

	return result, nil
}

// DownloadObject retrieves an object from the S3 bucket and writes it to w
func (s *Storage) DownloadObject(ctx context.Context, key string, w io.Writer, opts *cloudstorage.DownloadOptions) error {
	// Create GetObject input parameters
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	// Apply range header if options are provided
	if opts != nil && opts.Range != "" {
		input.Range = aws.String(opts.Range)
	}

	// Call the AWS S3 API
	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		s.log.Error("download object failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}
	defer func() { _ = output.Body.Close() }()

	// Copy the data to the provided writer
	if _, err = io.Copy(w, output.Body); err != nil {
		s.log.Error("write object data failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}

	return nil
}

// UploadObject uploads an object to the S3 bucket
func (s *Storage) UploadObject(ctx context.Context, key string, content io.Reader) error {
	// Create PutObject input parameters
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   content,
	}

	// Call the AWS S3 API
	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		s.log.Error("upload object failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}

	return nil
}

// DeleteObjects removes multiple objects from the S3 bucket
func (s *Storage) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	// Transform keys slice to S3 ObjectIdentifier slice
	objects := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i] = types.ObjectIdentifier{
			Key: aws.String(key),
		}
	}

	// Create DeleteObjects input parameters
	input := &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true), // Don't return details about each deletion
		},
	}

	// Call the AWS S3 API
	_, err := s.client.DeleteObjects(ctx, input)
	if err != nil {
		s.log.Error("delete objects failed",
			zap.Int("keyCount", len(keys)),
			zap.Error(err))
		return err
	}

	return nil
}

// PresignedGetURL generates a presigned URL for downloading an object from S3
func (s *Storage) PresignedGetURL(ctx context.Context, key string, opts *cloudstorage.PresignedGetOptions) (string, error) {
	// Set default expiration if not provided
	expiration := 15 * time.Minute
	if opts != nil && opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	// Create the presigner
	presigner := s3.NewPresignClient(s.client)

	// Create presigned GET URL input
	getInput := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	// Generate the presigned URL
	result, err := presigner.PresignGetObject(ctx, getInput, func(options *s3.PresignOptions) {
		options.Expires = expiration
	})
	if err != nil {
		s.log.Error("generate pre-signed get URL failed",
			zap.String("key", key),
			zap.Error(err))
		return "", err
	}

	return result.URL, nil
}

// PresignedPutURL generates a presigned URL for uploading an object to S3
func (s *Storage) PresignedPutURL(ctx context.Context, key string, opts *cloudstorage.PresignedPutOptions) (string, error) {
	// Set default expiration if not provided
	expiration := 15 * time.Minute
	if opts != nil && opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	// Create the presigner
	presigner := s3.NewPresignClient(s.client)

	// Create presigned PUT URL input
	putInput := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	// Set content type if provided
	if opts != nil && opts.ContentType != "" {
		putInput.ContentType = aws.String(opts.ContentType)
	}

	// Set content length constraint if provided
	if opts != nil && opts.ContentLength > 0 {
		putInput.ContentLength = aws.Int64(opts.ContentLength)
	}

	// Generate the presigned URL
	result, err := presigner.PresignPutObject(ctx, putInput, func(options *s3.PresignOptions) {
		options.Expires = expiration
	})
	if err != nil {
		s.log.Error("generate pre-signed put URL failed",
			zap.String("key", key),
			zap.Error(err))
		return "", err
	}

	return result.URL, nil
}
