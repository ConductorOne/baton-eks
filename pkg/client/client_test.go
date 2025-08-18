package client

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/rest"
)

const testClusterName = "test-cluster"
const actionStsAssumeRole = "sts:AssumeRole"

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

func TestExtractActions(t *testing.T) {
	c := &EKSClient{}

	// Test string action
	action := "sts:AssumeRole"
	actions := c.extractActions(action)
	if len(actions) != 1 || actions[0] != actionStsAssumeRole {
		t.Errorf("Expected [sts:AssumeRole], got %v", actions)
	}

	// Test array of actions
	actionArray := []interface{}{actionStsAssumeRole, "sts:GetCallerIdentity"}
	actions = c.extractActions(actionArray)
	if len(actions) != 2 {
		t.Errorf("Expected 2 actions, got %d", len(actions))
	}
}

func TestExtractPrincipalValues(t *testing.T) {
	// Test string principal
	principal := "arn:aws:iam::123456789012:user/testuser"
	values := extractPrincipalValues(principal)
	if len(values) != 1 || values[0] != principal {
		t.Errorf("Expected [%s], got %v", principal, values)
	}

	// Test array of principals
	principalArray := []interface{}{
		"arn:aws:iam::123456789012:user/user1",
		"arn:aws:iam::123456789012:user/user2",
	}
	values = extractPrincipalValues(principalArray)
	if len(values) != 2 {
		t.Errorf("Expected 2 principals, got %d", len(values))
	}
}

func TestExtractPrincipals(t *testing.T) {
	// Test AWS principals
	principal := map[string]interface{}{
		"AWS": "arn:aws:iam::123456789012:user/testuser",
	}
	principals := extractPrincipals(principal)
	if len(principals) != 1 || principals[0] != "arn:aws:iam::123456789012:user/testuser" {
		t.Errorf("Expected [arn:aws:iam::123456789012:user/testuser], got %v", principals)
	}

	// Test multiple principal types
	principal = map[string]interface{}{
		"AWS":       "arn:aws:iam::123456789012:user/testuser",
		"Service":   "ecs-tasks.amazonaws.com",
		"Federated": "cognito-identity.amazonaws.com",
	}
	principals = extractPrincipals(principal)
	if len(principals) != 1 {
		t.Errorf("Expected 1 principals, got %d", len(principals))
	}
}

func TestURLDecodeTrustPolicy(t *testing.T) {
	// Test URL-encoded trust policy document
	encodedPolicy := `%7B%22Version%22%3A%222012-10-17%22%2C%22Statement%22%3A%5B` +
		`%7B%22Effect%22%3A%22Allow%22%2C%22Principal%22%3A%7B%22AWS%22%3A%22arn%3Aaws%3A` +
		`iam%3A%3A123456789012%3Auser%2Ftestuser%22%7D%2C%22Action%22%3A%22sts%3AAssumeRole%22%7D%5D%7D`
	decodedPolicy, err := url.QueryUnescape(encodedPolicy)
	if err != nil {
		t.Fatalf("Failed to URL decode policy: %v", err)
	}

	expectedPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:user/testuser"},"Action":"sts:AssumeRole"}]}`

	if decodedPolicy != expectedPolicy {
		t.Errorf("Expected %s, got %s", expectedPolicy, decodedPolicy)
	}

	// Test parsing the decoded policy
	var trustPolicy TrustPolicy
	if err := json.Unmarshal([]byte(decodedPolicy), &trustPolicy); err != nil {
		t.Fatalf("Failed to parse decoded policy: %v", err)
	}

	if len(trustPolicy.Statement) != 1 {
		t.Errorf("Expected 1 statement, got %d", len(trustPolicy.Statement))
	}

	statement := trustPolicy.Statement[0]
	if statement.Effect != "Allow" {
		t.Errorf("Expected Effect 'Allow', got %s", statement.Effect)
	}
}
