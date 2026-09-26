package client

import (
	"context"
	"encoding/base64"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EKS validates the presigned STS URL inside the token, so its host and
// signed parameters must stay stable across SDK and authenticator upgrades.
func TestGenerateEKSTokenWithCredentials(t *testing.T) {
	tests := []struct {
		name     string
		region   string
		env      map[string]string
		wantHost string
	}{
		{name: "regional", region: "us-west-2", wantHost: "sts.us-west-2.amazonaws.com"},
		{name: "govcloud", region: "us-gov-west-1", wantHost: "sts.us-gov-west-1.amazonaws.com"},
		{
			name:     "fips",
			region:   "us-west-2",
			env:      map[string]string{"AWS_USE_FIPS_ENDPOINT": "true"},
			wantHost: "sts-fips.us-west-2.amazonaws.com",
		},
		{
			name:     "endpoint url ignored",
			region:   "us-west-2",
			env:      map[string]string{"AWS_ENDPOINT_URL": "https://localhost:4566", "AWS_ENDPOINT_URL_STS": "https://localhost:4566"},
			wantHost: "sts.us-west-2.amazonaws.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the developer's shared config and environment out of the test.
			missing := filepath.Join(t.TempDir(), "missing")
			t.Setenv("AWS_CONFIG_FILE", missing)
			t.Setenv("AWS_SHARED_CREDENTIALS_FILE", missing)
			t.Setenv("AWS_PROFILE", "")
			t.Setenv("AWS_USE_FIPS_ENDPOINT", "")
			t.Setenv("AWS_USE_DUALSTACK_ENDPOINT", "")
			t.Setenv("AWS_ENDPOINT_URL", "")
			t.Setenv("AWS_ENDPOINT_URL_STS", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			ctx := context.Background()
			awsCfg, err := config.LoadDefaultConfig(ctx,
				config.WithRegion("us-east-1"),
				config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", "session")),
			)
			require.NoError(t, err)

			token, err := GenerateEKSTokenWithCredentials(ctx, testClusterName, tt.region, awsCfg)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(token, "k8s-aws-v1."))

			raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
			require.NoError(t, err)
			u, err := url.Parse(string(raw))
			require.NoError(t, err)

			assert.Equal(t, "https", u.Scheme)
			assert.Equal(t, tt.wantHost, u.Host)
			q := u.Query()
			assert.Equal(t, "GetCallerIdentity", q.Get("Action"))
			assert.Equal(t, "2011-06-15", q.Get("Version"))
			assert.Equal(t, "60", q.Get("X-Amz-Expires"))
			assert.Equal(t, "host;x-k8s-aws-id", q.Get("X-Amz-SignedHeaders"))
			assert.Equal(t, "session", q.Get("X-Amz-Security-Token"))
			assert.Contains(t, q.Get("X-Amz-Credential"), "/"+tt.region+"/sts/aws4_request")
		})
	}
}
