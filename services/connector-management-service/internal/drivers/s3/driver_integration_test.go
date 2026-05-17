//go:build integration

package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
)

const (
	testBucket = "openfoundry-driver-test"
	testKey    = "prefix/hello.txt"
	testBody   = "hello-openfoundry"
)

// bootLocalstack stands up the LocalStack 3 container with the S3 service
// enabled and returns the endpoint URL accessible from the host.
func bootLocalstack(ctx context.Context, t *testing.T) string {
	t.Helper()
	container, err := localstack.Run(
		ctx,
		"localstack/localstack:3",
		testcontainers.WithEnv(map[string]string{
			"SERVICES":              "s3",
			"DEFAULT_REGION":        "us-east-1",
			"AWS_DEFAULT_REGION":    "us-east-1",
			"EAGER_SERVICE_LOADING": "1",
		}),
	)
	if err != nil {
		t.Fatalf("start localstack: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(shutdownCtx)
	})

	endpoint, err := container.PortEndpoint(ctx, "4566/tcp", "http")
	if err != nil {
		t.Fatalf("resolve localstack endpoint: %v", err)
	}
	return endpoint
}

func driverConfig(endpoint string) Config {
	return Config{
		Bucket:          testBucket,
		Endpoint:        endpoint,
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		PathStyle:       true,
	}
}

func seedBucket(ctx context.Context, t *testing.T, d *Driver, key, body string) {
	t.Helper()
	if _, err := d.client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(d.cfg.Bucket)}); err != nil {
		// CreateBucket is idempotent enough for our purposes; only fail on hard errors.
		if !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") && !strings.Contains(err.Error(), "BucketAlreadyExists") {
			t.Fatalf("create bucket: %v", err)
		}
	}
	if _, err := d.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(d.cfg.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte(body)),
	}); err != nil {
		t.Fatalf("put object: %v", err)
	}
}

func TestDriver_ConnectListRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	endpoint := bootLocalstack(ctx, t)

	d, err := New(ctx, driverConfig(endpoint))
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}

	// Driver.Connect against a fresh empty stack should fail (bucket missing).
	if err := d.Connect(ctx); err == nil {
		t.Fatalf("expected Connect to fail before bucket exists")
	}

	seedBucket(ctx, t, d, testKey, testBody)

	if err := d.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	objects, err := d.List(ctx, "prefix/", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("List returned %d objects, want 1", len(objects))
	}
	if objects[0].Key != testKey || objects[0].Size != int64(len(testBody)) {
		t.Fatalf("List entry mismatch: %+v", objects[0])
	}

	rc, err := d.Read(ctx, testKey)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != testBody {
		t.Fatalf("body = %q, want %q", body, testBody)
	}
}

func TestDriver_ConfigFromJSON_DerivesBucketFromURL(t *testing.T) {
	cfg, err := ConfigFromJSON(json.RawMessage(`{"url":"s3://my-bucket/sub/folder/","region":"eu-west-1"}`))
	if err != nil {
		t.Fatalf("ConfigFromJSON: %v", err)
	}
	if cfg.Bucket != "my-bucket" {
		t.Fatalf("bucket = %q", cfg.Bucket)
	}
	if cfg.Subfolder != "sub/folder" {
		t.Fatalf("subfolder = %q", cfg.Subfolder)
	}
	if cfg.Region != "eu-west-1" {
		t.Fatalf("region = %q", cfg.Region)
	}
}
