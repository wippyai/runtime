// main.go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// LocalStack default port for S3
	defaultS3Port = "4566"
	// Default region for S3
	defaultRegion = "us-east-1"
	// Dummy credentials for LocalStack
	accessKeyID     = "test"
	secretAccessKey = "test"
)

// LocalStackContainer represents a LocalStack container
type LocalStackContainer struct {
	testcontainers.Container
	URI string
}

// NewLocalStackContainer creates a new LocalStack container
func NewLocalStackContainer(ctx context.Context) (*LocalStackContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        "localstack/localstack:s3-latest",
		ExposedPorts: []string{defaultS3Port},
		Env: map[string]string{
			"AWS_ACCESS_KEY_ID":     "test",
			"AWS_SECRET_ACCESS_KEY": "test",
		},
		WaitingFor: wait.ForListeningPort(defaultS3Port),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, err
	}

	port, err := container.MappedPort(ctx, defaultS3Port)
	if err != nil {
		return nil, err
	}

	uri := fmt.Sprintf("http://%s:%s", host, port.Port())

	return &LocalStackContainer{
		Container: container,
		URI:       uri,
	}, nil
}

// CreateS3Client creates a new S3 client configured to use LocalStack
func CreateS3Client(ctx context.Context, localstackURI string) (*s3.Client, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:           localstackURI,
			SigningRegion: defaultRegion,
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(defaultRegion),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS SDK config: %w", err)
	}

	return s3.NewFromConfig(cfg), nil
}

// CreateBucket creates a new S3 bucket
func CreateBucket(ctx context.Context, client *s3.Client, bucketName string) error {
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
	}

	return nil
}

// UploadFile uploads a file to S3
func UploadFile(ctx context.Context, client *s3.Client, bucketName, fileName, contentType string, fileContent []byte) error {
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(fileName),
		Body:        strings.NewReader(string(fileContent)),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("failed to upload file %s to bucket %s: %w", fileName, bucketName, err)
	}

	return nil
}

// ListObjects lists all objects in a bucket
func ListObjects(ctx context.Context, client *s3.Client, bucketName string) ([]string, error) {
	resp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in bucket %s: %w", bucketName, err)
	}

	var objects []string
	for _, obj := range resp.Contents {
		objects = append(objects, *obj.Key)
	}

	return objects, nil
}

// ReadObject reads an object from a bucket
func ReadObject(ctx context.Context, client *s3.Client, bucketName, fileName string) ([]byte, error) {
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s from bucket %s: %w", fileName, bucketName, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func main() {
	ctx := context.Background()

	// Start LocalStack container
	log.Println("Starting LocalStack container...")
	localstack, err := NewLocalStackContainer(ctx)
	if err != nil {
		log.Fatalf("Failed to start LocalStack container: %v", err)
	}
	defer func() {
		if err := localstack.Terminate(ctx); err != nil {
			log.Printf("Failed to terminate LocalStack container: %v", err)
		}
	}()
	log.Printf("LocalStack running at: %s", localstack.URI)

	awsRegion := "us-east-1"

	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(awsRegion),
	)
	if err != nil {
		log.Fatalf("Cannot load the AWS configs: %s", err)
	}

	// Create the resource client
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(localstack.URI)
	})

	// Create a bucket
	bucketName := fmt.Sprintf("test-bucket-%d", time.Now().Unix())
	log.Printf("Creating bucket: %s", bucketName)
	if err := CreateBucket(ctx, s3Client, bucketName); err != nil {
		log.Fatalf("Failed to create bucket: %v", err)
	}

	// Upload a file
	fileName := "test-file.txt"
	fileContent := []byte("Hello, S3!")
	log.Printf("Uploading file: %s", fileName)
	if err := UploadFile(ctx, s3Client, bucketName, fileName, "text/plain", fileContent); err != nil {
		log.Fatalf("Failed to upload file: %v", err)
	}

	// List objects
	log.Println("Listing objects in bucket...")
	objects, err := ListObjects(ctx, s3Client, bucketName)
	if err != nil {
		log.Fatalf("Failed to list objects: %v", err)
	}
	log.Printf("Objects in bucket: %v", objects)

	// Read the uploaded file
	log.Printf("Reading file: %s", fileName)
	content, err := ReadObject(ctx, s3Client, bucketName, fileName)
	if err != nil {
		log.Fatalf("Failed to read object: %v", err)
	}
	log.Printf("File content: %s", string(content))

	log.Println("Done!")
}
