// main_test.go
package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3Upload(t *testing.T) {
	ctx := context.Background()

	// Start LocalStack container
	localstack, err := NewLocalStackContainer(ctx)
	require.NoError(t, err, "Failed to start LocalStack container")

	// Ensure container is terminated after test
	defer func() {
		if err := localstack.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate LocalStack container: %v", err)
		}
	}()

	awsEndpoint := "http://localhost:4566"
	awsRegion := "us-east-1"

	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(awsRegion),
	)
	if err != nil {
		log.Fatalf("Cannot load the AWS configs: %s", err)
	}

	// Create the resource client
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(awsEndpoint)
	})

	resp, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String("test")})
	fmt.Println(resp, err)

	return

	// Create S3 client
	s3Client, err := CreateS3Client(ctx, localstack.URI)
	require.NoError(t, err, "Failed to create S3 client")

	// Create a bucket with a unique name
	bucketName := fmt.Sprintf("test-bucket-%d", time.Now().Unix())
	err = CreateBucket(ctx, s3Client, bucketName)
	require.NoError(t, err, "Failed to create bucket")

	// Test file uploading
	t.Run("UploadAndReadFile", func(t *testing.T) {
		// Create test file
		fileName := "test-upload.txt"
		expectedContent := []byte("This is a test file for S3 upload")

		// Upload the file
		err := UploadFile(ctx, s3Client, bucketName, fileName, "text/plain", expectedContent)
		require.NoError(t, err, "Failed to upload file")

		// List objects to verify the file exists
		objects, err := ListObjects(ctx, s3Client, bucketName)
		require.NoError(t, err, "Failed to list objects")
		assert.Contains(t, objects, fileName, "Uploaded file not found in bucket")

		// Read the uploaded file
		content, err := ReadObject(ctx, s3Client, bucketName, fileName)
		require.NoError(t, err, "Failed to read object")
		assert.Equal(t, string(expectedContent), string(content), "File content doesn't match")
	})

	// Test uploading multiple files
	t.Run("UploadMultipleFiles", func(t *testing.T) {
		files := map[string][]byte{
			"file1.txt": []byte("Content of file 1"),
			"file2.txt": []byte("Content of file 2"),
			"file3.txt": []byte("Content of file 3"),
		}

		// Upload all files
		for fileName, content := range files {
			err := UploadFile(ctx, s3Client, bucketName, fileName, "text/plain", content)
			require.NoError(t, err, "Failed to upload file: %s", fileName)
		}

		// List objects
		objects, err := ListObjects(ctx, s3Client, bucketName)
		require.NoError(t, err, "Failed to list objects")

		// Verify all files are in the bucket
		for fileName := range files {
			assert.Contains(t, objects, fileName, "File %s not found in bucket", fileName)
		}

		// Read and verify content of each file
		for fileName, expectedContent := range files {
			content, err := ReadObject(ctx, s3Client, bucketName, fileName)
			require.NoError(t, err, "Failed to read object: %s", fileName)
			assert.Equal(t, string(expectedContent), string(content), "Content of %s doesn't match", fileName)
		}
	})
}
