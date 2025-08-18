package client

import (
	"fmt"
	"net/http"
	"sync"
	"time"

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
	exp           time.Time // Expiration time of the token

}

func (t *EksTokenRefreshRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Check if token is expired or not yet generated
	// EKS tokens last for ~15 minutes. We can generate a new one proactively.
	if t.token == "" || time.Until(t.exp) < 1*time.Minute {
		newToken, err := GenerateEKSToken(t.ClusterID, t.Region, t.AssumeRoleARN)
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

	tk, err := gen.GetWithOptions(opts)
	if err != nil {
		return "", fmt.Errorf("failed to get EKS token: %w", err)
	}

	return tk.Token, nil
}
