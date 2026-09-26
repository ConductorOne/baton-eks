package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	awsV2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

// EksTokenRefreshRoundTripper is a custom http.RoundTripper that refreshes EKS tokens.
type EksTokenRefreshRoundTripper struct {
	ClusterID     string
	Region        string
	AssumeRoleARN string
	Base          http.RoundTripper // The underlying HTTP transport
	token         string
	mu            sync.Mutex
	exp           time.Time    // Expiration time of the token
	AwsConfig     awsV2.Config // AWS config with assumed role credentials
}

func (t *EksTokenRefreshRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Check if token is expired or not yet generated
	// EKS tokens last for ~15 minutes. We can generate a new one proactively.
	if t.token == "" || time.Until(t.exp) < 1*time.Minute {
		newToken, err := GenerateEKSTokenWithCredentials(req.Context(), t.ClusterID, t.Region, t.AwsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh EKS token: %w", err)
		}
		t.token = newToken
		t.exp = time.Now().Add(15 * time.Minute) // Set expiration for 15 minutes
	}

	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.Base.RoundTrip(req)
}

// generateEKSToken generates a short-lived authentication token for EKS.
func GenerateEKSToken(clusterID string, region string, assumeRoleARN string) (string, error) {
	gen, err := token.NewGenerator(false, false) // `false, false` for disable_sts_regional_endpoints, disable_credential_cache
	if err != nil {
		return "", fmt.Errorf("failed to create EKS token generator: %w", err)
	}

	opts := &token.GetTokenOptions{
		ClusterID:     clusterID,
		Region:        region,
		AssumeRoleARN: assumeRoleARN,
	}

	tk, err := gen.GetWithOptions(context.Background(), opts)
	if err != nil {
		return "", fmt.Errorf("failed to get EKS token: %w", err)
	}

	return tk.Token, nil
}

// GenerateEKSTokenWithCredentials generates a short-lived authentication token for EKS using provided AWS credentials.
func GenerateEKSTokenWithCredentials(ctx context.Context, clusterID string, region string, awsConfig awsV2.Config) (string, error) {
	// Retrieve credentials from the AWS v2 config
	creds, err := awsConfig.Credentials.Retrieve(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve credentials: %w", err)
	}

	// Derive from awsConfig so env/shared-config endpoint settings (FIPS,
	// dual-stack) still apply to the presigned STS host. BaseEndpoint
	// (AWS_ENDPOINT_URL) is cleared: EKS only accepts tokens for real STS hosts.
	stsClient := sts.NewFromConfig(awsConfig, func(o *sts.Options) {
		o.Region = region
		o.BaseEndpoint = nil
		o.Credentials = credentials.NewStaticCredentialsProvider(
			creds.AccessKeyID,
			creds.SecretAccessKey,
			creds.SessionToken,
		)
	})

	// Create the token generator
	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return "", fmt.Errorf("failed to create EKS token generator: %w", err)
	}

	tk, err := gen.GetWithSTS(clusterID, stsClient)
	if err != nil {
		return "", fmt.Errorf("failed to get EKS token: %w", err)
	}

	return tk.Token, nil
}
