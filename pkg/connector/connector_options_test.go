package connector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	awsSdk "github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/conductorone/baton-eks/pkg/config"
	"github.com/stretchr/testify/require"
)

type injectedSTSClient struct {
	calls int
}

func (c *injectedSTSClient) AssumeRole(context.Context, *sts.AssumeRoleInput, ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	c.calls++
	expiresAt := time.Now().Add(time.Hour)
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{
		AccessKeyId:     awsSdk.String("access-key"),
		SecretAccessKey: awsSdk.String("secret-key"),
		SessionToken:    awsSdk.String("session-token"),
		Expiration:      &expiresAt,
	}}, nil
}

func TestNewUsesInjectedAWSConfigLoader(t *testing.T) {
	t.Parallel()

	loaderErr := errors.New("injected config loader")
	called := false
	_, err := New(t.Context(), &config.Eks{}, WithAWSConfigLoader(func(context.Context, ...func(*awsConfig.LoadOptions) error) (awsSdk.Config, error) {
		called = true
		return awsSdk.Config{}, loaderErr
	}))

	require.ErrorIs(t, err, loaderErr)
	require.True(t, called)
}

func TestInjectedSTSClientFactoryDrivesAssumeRole(t *testing.T) {
	t.Parallel()

	stsClient := &injectedSTSClient{}
	factoryCalls := 0
	connector := &Connector{
		config: &config.Eks{
			ExternalId:              "external-id",
			GlobalRoleArn:           "arn:aws:iam::123456789012:role/binding",
			GlobalBindingExternalId: "binding-external-id",
			RoleArn:                 "arn:aws:iam::123456789012:role/customer",
		},
		awsConfig: awsSdk.Config{Region: "us-west-2"},
		newSTSClient: func(awsSdk.Config) stscreds.AssumeRoleAPIClient {
			factoryCalls++
			return stsClient
		},
		_onceCallingConfig:  map[string]*sync.Once{},
		_callingConfig:      map[string]awsSdk.Config{},
		_callingConfigError: map[string]error{},
	}

	_, err := connector.getCallingConfig(t.Context(), "us-west-2")

	require.NoError(t, err)
	require.Equal(t, 2, factoryCalls)
	require.Equal(t, 2, stsClient.calls)
}

func TestNewRejectsNilAWSHooks(t *testing.T) {
	t.Parallel()

	_, err := New(t.Context(), &config.Eks{}, WithAWSConfigLoader(nil))
	require.EqualError(t, err, "eks connector: AWS config loader and STS client factory are required")

	_, err = New(t.Context(), &config.Eks{}, WithSTSClientFactory(nil))
	require.EqualError(t, err, "eks connector: AWS config loader and STS client factory are required")
}
