// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wippyai/runtime/api/cloudstorage"
)

const (
	// maxSingleCopySize is the largest source a single CopyObject request accepts (5 GiB).
	maxSingleCopySize int64 = 5 << 30
	// defaultCopyPartSize is the UploadPartCopy range size for sources above maxSingleCopySize.
	defaultCopyPartSize int64 = 1 << 30
)

var _ cloudstorage.CopyStorage = (*Storage)(nil)

// CopyObject copies srcKey to dstKey inside the bucket. Sources above 5 GiB are
// copied with a multipart upload of UploadPartCopy ranges, so the call succeeds
// for every object size the provider stores.
func (s *Storage) CopyObject(ctx context.Context, srcKey, dstKey string, opts *cloudstorage.CopyOptions) (*cloudstorage.CopyResult, error) {
	if opts == nil {
		opts = &cloudstorage.CopyOptions{}
	}
	directive := opts.MetadataDirective
	if directive == "" {
		directive = cloudstorage.MetadataDirectiveCopy
	}
	if err := validateCopy(srcKey, dstKey, directive, opts); err != nil {
		return nil, err
	}

	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return nil, s.fail(ctx, "copy object", srcKey, err)
	}

	if aws.ToInt64(head.ContentLength) > s.copySingleLimit {
		return s.copyMultipart(ctx, srcKey, dstKey, directive, opts, head)
	}

	input := &s3.CopyObjectInput{
		Bucket:            aws.String(s.bucket),
		Key:               aws.String(dstKey),
		CopySource:        aws.String(copySource(s.bucket, srcKey)),
		MetadataDirective: types.MetadataDirective(directive),
	}
	if opts.IfMatch != "" {
		input.CopySourceIfMatch = aws.String(opts.IfMatch)
	}
	if opts.IfNoneMatch != "" {
		input.IfNoneMatch = aws.String(opts.IfNoneMatch)
	}
	if directive == cloudstorage.MetadataDirectiveReplace {
		input.Metadata = copyMap(opts.Metadata)
		if opts.ContentType != "" {
			input.ContentType = aws.String(opts.ContentType)
		}
	}

	output, err := s.client.CopyObject(ctx, input)
	if err != nil {
		return nil, s.fail(ctx, "copy object", srcKey, err)
	}

	result := &cloudstorage.CopyResult{}
	if output.CopyObjectResult != nil {
		result.ETag = aws.ToString(output.CopyObjectResult.ETag)
		if output.CopyObjectResult.LastModified != nil {
			result.LastModified = *output.CopyObjectResult.LastModified
		}
	}
	return result, nil
}

func validateCopy(srcKey, dstKey string, directive cloudstorage.MetadataDirective, opts *cloudstorage.CopyOptions) error {
	switch {
	case srcKey == "":
		return NewInvalidRequestError("source key is required")
	case dstKey == "":
		return NewInvalidRequestError("destination key is required")
	case directive != cloudstorage.MetadataDirectiveCopy && directive != cloudstorage.MetadataDirectiveReplace:
		return NewInvalidRequestError("metadata directive must be COPY or REPLACE")
	case directive == cloudstorage.MetadataDirectiveCopy && (len(opts.Metadata) > 0 || opts.ContentType != ""):
		return NewInvalidRequestError("metadata and content type require the REPLACE metadata directive")
	case directive == cloudstorage.MetadataDirectiveCopy && srcKey == dstKey:
		return NewInvalidRequestError("copying an object onto itself requires the REPLACE metadata directive")
	}
	return nil
}

// copyMultipart copies a source larger than a single CopyObject request allows.
// Every part is pinned to the source ETag observed at the start of the copy, or
// to the caller's if_match, so the destination is assembled from one version.
func (s *Storage) copyMultipart(
	ctx context.Context,
	srcKey, dstKey string,
	directive cloudstorage.MetadataDirective,
	opts *cloudstorage.CopyOptions,
	head *s3.HeadObjectOutput,
) (*cloudstorage.CopyResult, error) {
	create := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(dstKey),
	}
	if directive == cloudstorage.MetadataDirectiveReplace {
		create.Metadata = copyMap(opts.Metadata)
		if opts.ContentType != "" {
			create.ContentType = aws.String(opts.ContentType)
		}
	} else {
		create.Metadata = copyMap(head.Metadata)
		create.ContentType = head.ContentType
		create.CacheControl = head.CacheControl
		create.ContentDisposition = head.ContentDisposition
		create.ContentEncoding = head.ContentEncoding
	}

	created, err := s.client.CreateMultipartUpload(ctx, create)
	if err != nil {
		return nil, s.fail(ctx, "copy object", dstKey, err)
	}
	uploadID := created.UploadId

	completed, err := s.copyParts(ctx, srcKey, dstKey, uploadID, opts, head)
	if err == nil {
		completeInput := &s3.CompleteMultipartUploadInput{
			Bucket:          aws.String(s.bucket),
			Key:             aws.String(dstKey),
			UploadId:        uploadID,
			MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
		}
		if opts.IfNoneMatch != "" {
			completeInput.IfNoneMatch = aws.String(opts.IfNoneMatch)
		}
		var output *s3.CompleteMultipartUploadOutput
		if output, err = s.client.CompleteMultipartUpload(ctx, completeInput); err == nil {
			return s.copyResult(ctx, dstKey, aws.ToString(output.ETag))
		}
	}

	_, _ = s.client.AbortMultipartUpload(context.WithoutCancel(ctx), &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(dstKey),
		UploadId: uploadID,
	})
	return nil, s.fail(ctx, "copy object", srcKey, err)
}

func (s *Storage) copyParts(
	ctx context.Context,
	srcKey, dstKey string,
	uploadID *string,
	opts *cloudstorage.CopyOptions,
	head *s3.HeadObjectOutput,
) ([]types.CompletedPart, error) {
	pin := aws.ToString(head.ETag)
	if opts.IfMatch != "" {
		pin = opts.IfMatch
	}
	size := aws.ToInt64(head.ContentLength)
	source := copySource(s.bucket, srcKey)

	var parts []types.CompletedPart
	for offset, number := int64(0), int32(1); offset < size; offset, number = offset+s.copyPartSize, number+1 {
		last := min(offset+s.copyPartSize, size) - 1
		output, err := s.client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
			Bucket:            aws.String(s.bucket),
			Key:               aws.String(dstKey),
			UploadId:          uploadID,
			PartNumber:        aws.Int32(number),
			CopySource:        aws.String(source),
			CopySourceRange:   aws.String("bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(last, 10)),
			CopySourceIfMatch: aws.String(pin),
		})
		if err != nil {
			return nil, err
		}
		parts = append(parts, types.CompletedPart{
			ETag:       output.CopyPartResult.ETag,
			PartNumber: aws.Int32(number),
		})
	}
	return parts, nil
}

// copyResult reads the destination's modification time, which a completed
// multipart upload does not report.
func (s *Storage) copyResult(ctx context.Context, dstKey, etag string) (*cloudstorage.CopyResult, error) {
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(dstKey),
	})
	if err != nil {
		return nil, s.fail(ctx, "copy object", dstKey, err)
	}
	result := &cloudstorage.CopyResult{ETag: etag}
	if head.LastModified != nil {
		result.LastModified = *head.LastModified
	}
	return result, nil
}

// copySource builds the URL-encoded x-amz-copy-source value for a key.
func copySource(bucket, key string) string {
	segments := strings.Split(bucket+"/"+key, "/")
	for i, segment := range segments {
		segments[i] = strings.ReplaceAll(url.QueryEscape(segment), "+", "%20")
	}
	return strings.Join(segments, "/")
}

func copyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
