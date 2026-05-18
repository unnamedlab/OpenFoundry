package kms

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

const (
	awsKMSDefaultTimeout = 5 * time.Second
	awsKMSEncryptTarget  = "TrentService.Encrypt"
	awsKMSDecryptTarget  = "TrentService.Decrypt"
)

type awsEncryptDecryptClient interface {
	Encrypt(context.Context, *awsKMSEncryptInput) (*awsKMSEncryptOutput, error)
	Decrypt(context.Context, *awsKMSDecryptInput) (*awsKMSDecryptOutput, error)
}

type awsKMSEncryptInput struct {
	KeyID     string
	Plaintext []byte
}

type awsKMSEncryptOutput struct {
	CiphertextBlob []byte
}

type awsKMSDecryptInput struct {
	CiphertextBlob []byte
}

type awsKMSDecryptOutput struct {
	Plaintext []byte
}

// AWSKMSClient wraps DEKs with AWS KMS Encrypt/Decrypt. The public KMS
// interface intentionally does not accept a context, so each operation runs
// with a short bounded background timeout to avoid indefinitely stalled AWS
// calls while keeping key material out of logs and error messages.
type AWSKMSClient struct {
	keyARN  string
	client  awsEncryptDecryptClient
	timeout time.Duration
}

func NewAWSKMSClient(ctx context.Context, region, keyARN, endpointOverride string) (*AWSKMSClient, error) {
	keyARN = strings.TrimSpace(keyARN)
	if keyARN == "" {
		return nil, ErrAWSKeyMissing
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if region = strings.TrimSpace(region); region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("cipher kms: load aws config: %w", err)
	}
	if awsCfg.Region == "" {
		awsCfg.Region = regionFromARN(keyARN)
	}
	if awsCfg.Region == "" {
		awsCfg.Region = "us-east-1"
	}

	return newAWSKMSClientWithClient(keyARN, &awsKMSHTTPClient{
		region:   awsCfg.Region,
		endpoint: kmsEndpoint(awsCfg.Region, endpointOverride),
		creds:    awsCfg.Credentials,
		signer:   v4.NewSigner(),
		http:     awsCfg.HTTPClient,
	}, awsKMSDefaultTimeout), nil
}

func newAWSKMSClientWithClient(keyARN string, client awsEncryptDecryptClient, timeout time.Duration) *AWSKMSClient {
	if timeout <= 0 {
		timeout = awsKMSDefaultTimeout
	}
	return &AWSKMSClient{keyARN: strings.TrimSpace(keyARN), client: client, timeout: timeout}
}

func (c *AWSKMSClient) Wrap(plainDEK []byte) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("cipher kms: aws client is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	out, err := c.client.Encrypt(ctx, &awsKMSEncryptInput{KeyID: c.keyARN, Plaintext: plainDEK})
	if err != nil {
		return nil, fmt.Errorf("cipher kms: aws encrypt failed: %w", err)
	}
	return out.CiphertextBlob, nil
}

func (c *AWSKMSClient) Unwrap(wrapped []byte) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("cipher kms: aws client is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	out, err := c.client.Decrypt(ctx, &awsKMSDecryptInput{CiphertextBlob: wrapped})
	if err != nil {
		return nil, fmt.Errorf("cipher kms: aws decrypt failed: %w", err)
	}
	return out.Plaintext, nil
}

func (c *AWSKMSClient) Ref() string { return "aws:kms:" + c.keyARN }

type awsKMSHTTPClient struct {
	region   string
	endpoint string
	creds    aws.CredentialsProvider
	signer   *v4.Signer
	http     aws.HTTPClient
}

func (c *awsKMSHTTPClient) Encrypt(ctx context.Context, in *awsKMSEncryptInput) (*awsKMSEncryptOutput, error) {
	var out struct {
		CiphertextBlob string `json:"CiphertextBlob"`
	}
	if err := c.invoke(ctx, awsKMSEncryptTarget, map[string]string{
		"KeyId":     in.KeyID,
		"Plaintext": base64.StdEncoding.EncodeToString(in.Plaintext),
	}, &out); err != nil {
		return nil, err
	}
	blob, err := base64.StdEncoding.DecodeString(out.CiphertextBlob)
	if err != nil {
		return nil, fmt.Errorf("decode encrypt response: %w", err)
	}
	return &awsKMSEncryptOutput{CiphertextBlob: blob}, nil
}

func (c *awsKMSHTTPClient) Decrypt(ctx context.Context, in *awsKMSDecryptInput) (*awsKMSDecryptOutput, error) {
	var out struct {
		Plaintext string `json:"Plaintext"`
	}
	if err := c.invoke(ctx, awsKMSDecryptTarget, map[string]string{
		"CiphertextBlob": base64.StdEncoding.EncodeToString(in.CiphertextBlob),
	}, &out); err != nil {
		return nil, err
	}
	plain, err := base64.StdEncoding.DecodeString(out.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("decode decrypt response: %w", err)
	}
	return &awsKMSDecryptOutput{Plaintext: plain}, nil
}

func (c *awsKMSHTTPClient) invoke(ctx context.Context, target string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", target)

	creds, err := c.creds.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("retrieve credentials: %w", err)
	}
	hash := sha256.Sum256(body)
	if err := c.signer.SignHTTP(ctx, creds, req, hex.EncodeToString(hash[:]), "kms", c.region, time.Now()); err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	httpClient := c.http
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return kmsHTTPError(resp.StatusCode, respBody)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func kmsEndpoint(region, endpointOverride string) string {
	if endpoint := strings.TrimSpace(endpointOverride); endpoint != "" {
		return endpoint
	}
	return "https://kms." + region + ".amazonaws.com"
}

func regionFromARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) >= 4 && parts[0] == "arn" && parts[2] == "kms" {
		return parts[3]
	}
	return ""
}

func kmsHTTPError(status int, body []byte) error {
	var payload struct {
		Type    string `json:"__type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)
	code := payload.Code
	if code == "" {
		code = payload.Type
	}
	if idx := strings.LastIndex(code, "#"); idx >= 0 {
		code = code[idx+1:]
	}
	if code == "" {
		code = http.StatusText(status)
	}
	if payload.Message != "" {
		return fmt.Errorf("kms http %d %s: %s", status, code, payload.Message)
	}
	return fmt.Errorf("kms http %d %s", status, code)
}
