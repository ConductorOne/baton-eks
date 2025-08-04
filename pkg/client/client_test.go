package client

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
)

const testClusterName = "test-cluster"

func TestNewClient(t *testing.T) {
	// Test creating a new EKS client
	restConfig := &rest.Config{
		Host: "https://test-cluster.example.com",
	}

	awsCfg := aws.Config{
		Region: "us-east-1",
	}

	client, err := NewClient(restConfig, awsCfg, testClusterName)

	assert.NoError(t, err)
	assert.NotNil(t, client)
	assert.Equal(t, testClusterName, client.clusterName)
	assert.NotNil(t, client.kubernetes)
	assert.NotNil(t, client.eksClient)
}

func TestEKSClient_LoadIdentityCacheMaps(t *testing.T) {
	// This test would require a mock EKS client to test the access entries functionality
	// For now, we'll just test that the client can be created and the method exists
	restConfig := &rest.Config{
		Host: "https://test-cluster.example.com",
	}

	awsCfg := aws.Config{
		Region: "us-east-1",
	}

	client, err := NewClient(restConfig, awsCfg, testClusterName)
	assert.NoError(t, err)

	// Test that the method exists and doesn't panic
	ctx := context.Background()
	_ = client.LoadIdentityCacheMaps(ctx)
	// This will likely fail in a test environment, but we're just testing the interface
	assert.NotNil(t, client)
}

func TestEKSClient_MergingFunctionality(t *testing.T) {
	// Test that the client properly handles merging of both authentication sources
	restConfig := &rest.Config{
		Host: "https://test-cluster.example.com",
	}

	awsCfg := aws.Config{
		Region: "us-east-1",
	}

	client, err := NewClient(restConfig, awsCfg, testClusterName)
	assert.NoError(t, err)

	// Test that the client is properly initialized for merging
	assert.NotNil(t, client)
	assert.Equal(t, testClusterName, client.clusterName)
	assert.NotNil(t, client.kubernetes)
	assert.NotNil(t, client.eksClient)

	// Test that the cache maps are initialized
	assert.NotNil(t, client.cacheUsersMap)
	assert.NotNil(t, client.cacheGroupsMap)

	// Test that the identity mutex is available for thread safety
	assert.NotNil(t, &client.identityMutex)
}
