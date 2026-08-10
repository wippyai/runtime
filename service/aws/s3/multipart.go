// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/middleware"
	"github.com/wippyai/runtime/api/cloudstorage"
	"go.uber.org/zap"
)

var _ cloudstorage.MultipartStorage = (*Storage)(nil)

func (s *Storage) CreateMultipartUpload(ctx context.Context, key string, opts *cloudstorage.CreateMultipartUploadOptions) (*cloudstorage.CreateMultipartUploadResult, error) {
	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	var apiOptions []func(*s3.Options)
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
		if len(opts.Metadata) > 0 {
			input.Metadata = make(map[string]string, len(opts.Metadata))
			for k, v := range opts.Metadata {
				input.Metadata[k] = v
			}
		}
		if len(opts.Headers) > 0 {
			mw := &addRequestHeadersMiddleware{headers: opts.Headers}
			apiOptions = append(apiOptions, func(o *s3.Options) {
				o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
					return stack.Build.Add(mw, middleware.After)
				})
			})
		}
	}

	output, err := s.client.CreateMultipartUpload(ctx, input, apiOptions...)
	if err != nil {
		s.log.Error("create multipart upload failed",
			zap.String("key", key),
			zap.Error(err))
		return nil, err
	}

	return &cloudstorage.CreateMultipartUploadResult{
		UploadID: aws.ToString(output.UploadId),
	}, nil
}

func (s *Storage) PresignedUploadPartURLs(ctx context.Context, key, uploadID string, opts *cloudstorage.PresignedUploadPartOptions) ([]cloudstorage.PresignedPartURL, error) {
	if uploadID == "" {
		return nil, errors.New("upload ID is required")
	}
	if opts == nil || len(opts.PartNumbers) == 0 {
		return nil, errors.New("at least one part number is required")
	}
	if len(opts.PartNumbers) > cloudstorage.MaxPresignPartBatch {
		return nil, fmt.Errorf("at most %d part URLs per call, got %d",
			cloudstorage.MaxPresignPartBatch, len(opts.PartNumbers))
	}

	expiration := DefaultPresignExpiration
	if opts.Expiration > 0 {
		expiration = opts.Expiration
	}

	presigner := s3.NewPresignClient(s.client)

	urls := make([]cloudstorage.PresignedPartURL, 0, len(opts.PartNumbers))
	for _, partNumber := range opts.PartNumbers {
		if partNumber < 1 || partNumber > cloudstorage.MaxPartNumber {
			return nil, fmt.Errorf("part number %d out of range [1, %d]",
				partNumber, cloudstorage.MaxPartNumber)
		}

		input := &s3.UploadPartInput{
			Bucket:     aws.String(s.bucket),
			Key:        aws.String(key),
			UploadId:   aws.String(uploadID),
			PartNumber: aws.Int32(partNumber),
		}

		result, err := presigner.PresignUploadPart(ctx, input, func(options *s3.PresignOptions) {
			options.Expires = expiration
		})
		if err != nil {
			s.log.Error("generate pre-signed upload part URL failed",
				zap.String("key", key),
				zap.Int32("partNumber", partNumber),
				zap.Error(err))
			return nil, err
		}

		urls = append(urls, cloudstorage.PresignedPartURL{
			PartNumber: partNumber,
			URL:        result.URL,
		})
	}

	return urls, nil
}

func (s *Storage) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []cloudstorage.CompletedPart) (*cloudstorage.CompleteMultipartUploadResult, error) {
	if uploadID == "" {
		return nil, errors.New("upload ID is required")
	}
	if len(parts) == 0 {
		return nil, errors.New("at least one completed part is required")
	}

	completed := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		if p.PartNumber < 1 || p.PartNumber > cloudstorage.MaxPartNumber {
			return nil, fmt.Errorf("part number %d out of range [1, %d]",
				p.PartNumber, cloudstorage.MaxPartNumber)
		}
		if p.ETag == "" {
			return nil, fmt.Errorf("part %d is missing its etag", p.PartNumber)
		}
		completed[i] = types.CompletedPart{
			ETag:       aws.String(p.ETag),
			PartNumber: aws.Int32(p.PartNumber),
		}
	}

	sort.Slice(completed, func(i, j int) bool {
		return aws.ToInt32(completed[i].PartNumber) < aws.ToInt32(completed[j].PartNumber)
	})

	input := &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completed,
		},
	}

	output, err := s.client.CompleteMultipartUpload(ctx, input)
	if err != nil {
		if mapped := mapKnownError(err); errors.Is(mapped, cloudstorage.ErrNotFound) {
			return nil, mapped
		}
		s.log.Error("complete multipart upload failed",
			zap.String("key", key),
			zap.Error(err))
		return nil, err
	}

	return &cloudstorage.CompleteMultipartUploadResult{
		ETag:      aws.ToString(output.ETag),
		VersionID: aws.ToString(output.VersionId),
		Location:  aws.ToString(output.Location),
	}, nil
}

func (s *Storage) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	if uploadID == "" {
		return errors.New("upload ID is required")
	}

	input := &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	}

	if _, err := s.client.AbortMultipartUpload(ctx, input); err != nil {
		if mapped := mapKnownError(err); errors.Is(mapped, cloudstorage.ErrNotFound) {
			return mapped
		}
		s.log.Error("abort multipart upload failed",
			zap.String("key", key),
			zap.Error(err))
		return err
	}

	return nil
}
