// SPDX-License-Identifier: MPL-2.0

package s3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestD06S3DeleteObjectsReportsServiceFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		_, hasDelete := r.URL.Query()["delete"]
		require.True(t, hasDelete)
		w.Header().Set("Content-Type", "application/xml")
		_, err := w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Error><Key>literal/a</Key><Code>AccessDenied</Code><Message>denied</Message></Error>
  <Error><Key>literal b</Key><Code>InternalError</Code><Message>failed</Message></Error>
</DeleteResult>`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	cfg := aws.Config{
		Region:       "test",
		BaseEndpoint: aws.String(server.URL),
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
		}),
	}
	client := awss3.NewFromConfig(cfg, func(o *awss3.Options) { o.UsePathStyle = true })
	storage := NewStorage(client, "bucket", zap.NewNop())

	err := storage.DeleteObjects(context.Background(), []string{"literal/a", "literal b"})
	require.Error(t, err)
	for _, literal := range []string{"literal/a", "AccessDenied", "literal b", "InternalError"} {
		require.True(t, strings.Contains(err.Error(), literal), "error %q must name %q", err, literal)
	}
}
