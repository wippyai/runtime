// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wippyai/runtime/api/cloudstorage"
	"go.uber.org/zap"
)

// DefaultPresignExpiration is the default expiration time for presigned URLs.
const DefaultPresignExpiration = 15 * time.Minute

// Compile-time interface check.
var _ cloudstorage.Storage = (*Storage)(nil)

// Storage implements the cloudstorage.Storage interface for AWS S3
type Storage struct {
	client *s3.Client
	log    *zap.Logger
	bucket string
}

// NewStorage creates a new Storage instance.
func NewStorage(client *s3.Client, bucket string, log *zap.Logger) *Storage {
	if log == nil {
		log = zap.NewNop()
	}
	return &Storage{
		client: client,
		bucket: bucket,
		log:    log.With(zap.String("component", "s3storage"), zap.String("bucket", bucket)),
	}
}

// ListObjects lists objects in the S3 bucket with the given options.
func (s *Storage) ListObjects(ctx context.Context, opts *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	if opts != nil && opts.IncludeVersions {
		return s.listObjectVersions(ctx, opts)
	}
	return s.listObjectsV2(ctx, opts)
}

func (s *Storage) listObjectsV2(ctx context.Context, opts *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	}

	if opts != nil {
		if opts.Prefix != "" {
			input.Prefix = aws.String(opts.Prefix)
		}
		if opts.MaxKeys > 0 {
			input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
		}
		if opts.ContinuationToken != "" {
			input.ContinuationToken = aws.String(opts.ContinuationToken)
		}
		if opts.IncludeOwner {
			input.FetchOwner = aws.Bool(true)
		}
	}

	output, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		s.log.Error("list objects failed", zap.Error(err))
		return nil, err
	}

	result := &cloudstorage.ListObjectsResult{
		IsTruncated:           aws.ToBool(output.IsTruncated),
		NextContinuationToken: aws.ToString(output.NextContinuationToken),
		Objects:               make([]cloudstorage.ObjectMetadata, 0, len(output.Contents)),
	}

	for _, item := range output.Contents {
		obj := cloudstorage.ObjectMetadata{
			Key:          aws.ToString(item.Key),
			Size:         aws.ToInt64(item.Size),
			ETag:         aws.ToString(item.ETag),
			StorageClass: string(item.StorageClass),
			// ContentType is not available in ListObjectsV2 response.
		}
		if item.LastModified != nil {
			obj.LastModified = *item.LastModified
		}
		if item.Owner != nil {
			obj.Owner = &cloudstorage.Owner{
				ID:          aws.ToString(item.Owner.ID),
				DisplayName: aws.ToString(item.Owner.DisplayName),
			}
		}
		result.Objects = append(result.Objects, obj)
	}

	return result, nil
}

func (s *Storage) listObjectVersions(ctx context.Context, opts *cloudstorage.ListObjectsOptions) (*cloudstorage.ListObjectsResult, error) {
	input := &s3.ListObjectVersionsInput{
		Bucket: aws.String(s.bucket),
	}

	if opts != nil {
		if opts.Prefix != "" {
			input.Prefix = aws.String(opts.Prefix)
		}
		if opts.MaxKeys > 0 {
			input.MaxKeys = aws.Int32(int32(opts.MaxKeys))
		}
		if opts.ContinuationToken != "" {
			input.KeyMarker = aws.String(opts.ContinuationToken)
		}
	}

	output, err := s.client.ListObjectVersions(ctx, input)
	if err != nil {
		s.log.Error("list object versions failed", zap.Error(err))
		return nil, err
	}

	result := &cloudstorage.ListObjectsResult{
		IsTruncated:           aws.ToBool(output.IsTruncated),
		NextContinuationToken: aws.ToString(output.NextKeyMarker),
		Objects:               make([]cloudstorage.ObjectMetadata, 0, len(output.Versions)),
	}

	for _, v := range output.Versions {
		obj := cloudstorage.ObjectMetadata{
			Key:          aws.ToString(v.Key),
			Size:         aws.ToInt64(v.Size),
			ETag:         aws.ToString(v.ETag),
			StorageClass: string(v.StorageClass),
			VersionID:    aws.ToString(v.VersionId),
		}
		if v.LastModified != nil {
			obj.LastModified = *v.LastModified
		}
		if v.Owner != nil {
			obj.Owner = &cloudstorage.Owner{
				ID:          aws.ToString(v.Owner.ID),
				DisplayName: aws.ToString(v.Owner.DisplayName),
			}
		}
		result.Objects = append(result.Objects, obj)
	}

	return result, nil
}

// HeadObject fetches full metadata for a single object.
func (s *Storage) HeadObject(ctx context.Context, key string) (*cloudstorage.HeadObjectResult, error) {
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if mapped := mapKnownError(err); errors.Is(mapped, cloudstorage.ErrNotFound) {
			return nil, mapped
		}
		s.log.Error("head object failed",
			zap.String("key", key),
			zap.Error(err))
		return nil, err
	}

	res := &cloudstorage.HeadObjectResult{
		Size:               aws.ToInt64(output.ContentLength),
		ETag:               aws.ToString(output.ETag),
		ContentType:        aws.ToString(output.ContentType),
		CacheControl:       aws.ToString(output.CacheControl),
		ContentDisposition: aws.ToString(output.ContentDisposition),
		ContentEncoding:    aws.ToString(output.ContentEncoding),
		StorageClass:       string(output.StorageClass),
		VersionID:          aws.ToString(output.VersionId),
	}
	if output.LastModified != nil {
		res.LastModified = *output.LastModified
	}
	if len(output.Metadata) > 0 {
		res.UserMetadata = make(map[string]string, len(output.Metadata))
		for k, v := range output.Metadata {
			res.UserMetadata[k] = v
		}
	}

	return res, nil
}

// DownloadObject retrieves an object from the S3 bucket and writes it to w.
func (s *Storage) DownloadObject(ctx context.Context, key string, w io.Writer, opts *cloudstorage.DownloadOptions) error {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	if opts != nil {
		if opts.Range != "" {
			input.Range = aws.String(opts.Range)
		}
		if opts.IfMatch != "" {
			input.IfMatch = aws.String(opts.IfMatch)
		}
		if opts.IfNoneMatch != "" {
			input.IfNoneMatch = aws.String(opts.IfNoneMatch)
		}
	}

	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		mapped := mapKnownError(err)
		if errors.Is(mapped, cloudstorage.ErrPreconditionFailed) || errors.Is(mapped, cloudstorage.ErrNotFound) {
			return mapped
		}
		s.log.Error("download object failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}
	defer func() { _ = output.Body.Close() }()

	if _, err = io.Copy(w, output.Body); err != nil {
		s.log.Error("write object data failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}

	return nil
}

// UploadObject uploads an object to the S3 bucket.
func (s *Storage) UploadObject(ctx context.Context, key string, content io.Reader, opts *cloudstorage.UploadOptions) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   content,
	}

	if opts != nil {
		if opts.ContentType != "" {
			input.ContentType = aws.String(opts.ContentType)
		}
		if opts.CacheControl != "" {
			input.CacheControl = aws.String(opts.CacheControl)
		}
		if opts.ContentDisposition != "" {
			input.ContentDisposition = aws.String(opts.ContentDisposition)
		}
		if opts.ContentEncoding != "" {
			input.ContentEncoding = aws.String(opts.ContentEncoding)
		}
		if opts.IfMatch != "" {
			input.IfMatch = aws.String(opts.IfMatch)
		}
		if opts.IfNoneMatch != "" {
			input.IfNoneMatch = aws.String(opts.IfNoneMatch)
		}
		if len(opts.Metadata) > 0 {
			input.Metadata = make(map[string]string, len(opts.Metadata))
			for k, v := range opts.Metadata {
				input.Metadata[k] = v
			}
		}
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		if mapped := mapKnownError(err); errors.Is(mapped, cloudstorage.ErrPreconditionFailed) {
			return mapped
		}
		s.log.Error("upload object failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}

	return nil
}

// DeleteObjects removes multiple objects from the S3 bucket.
func (s *Storage) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	objects := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i] = types.ObjectIdentifier{
			Key: aws.String(key),
		}
	}

	input := &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true),
		},
	}

	_, err := s.client.DeleteObjects(ctx, input)
	if err != nil {
		s.log.Error("delete objects failed",
			zap.Int("keyCount", len(keys)),
			zap.Error(err))
		return err
	}

	return nil
}

// PresignedGetURL generates a presigned URL for downloading an object from S3.
func (s *Storage) PresignedGetURL(ctx context.Context, key string, opts *cloudstorage.PresignedGetOptions) (string, error) {
	expiration := DefaultPresignExpiration
	if opts != nil && opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	presigner := s3.NewPresignClient(s.client)

	getInput := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

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

// PresignedPutURL generates a presigned URL for uploading an object to S3.
func (s *Storage) PresignedPutURL(ctx context.Context, key string, opts *cloudstorage.PresignedPutOptions) (string, error) {
	expiration := DefaultPresignExpiration
	if opts != nil && opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	presigner := s3.NewPresignClient(s.client)

	putInput := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	if opts != nil && opts.ContentType != "" {
		putInput.ContentType = aws.String(opts.ContentType)
	}

	if opts != nil && opts.ContentLength > 0 {
		putInput.ContentLength = aws.Int64(opts.ContentLength)
	}

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

// mapKnownError translates S3 SDK errors into the typed sentinels exposed by
// the cloudstorage package: 404 / NoSuchKey / NotFound → ErrNotFound,
// 412 / 304 → ErrPreconditionFailed. Other errors pass through unchanged.
func mapKnownError(err error) error {
	if err == nil {
		return nil
	}

	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return cloudstorage.ErrNotFound
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return cloudstorage.ErrNotFound
	}

	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case http.StatusNotFound:
			return cloudstorage.ErrNotFound
		case http.StatusPreconditionFailed, http.StatusNotModified:
			return cloudstorage.ErrPreconditionFailed
		}
	}
	return err
}
