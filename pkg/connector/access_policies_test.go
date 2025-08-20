package connector

import (
	"testing"

	"github.com/conductorone/baton-eks/pkg/client"
	"github.com/stretchr/testify/assert"
)

func TestPolicyBuilder_ResourceType(t *testing.T) {
	// Create a mock EKS client
	eksClient := &client.EKSClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Test resource type
	resourceType := builder.ResourceType(t.Context())
	assert.Equal(t, "access_policy", resourceType.Id)
	assert.Equal(t, "Access Policy", resourceType.DisplayName)
	assert.Equal(t, "EKS Access Policy", resourceType.Description)
}

func TestPolicyBuilder_Entitlements(t *testing.T) {
	// Create a mock EKS client
	eksClient := &client.EKSClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Create a test resource
	resource, err := builder.policyResource(&client.AccessPolicy{
		PolicyARN: "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
		AccessScope: client.AccessScope{
			Type: "cluster",
		},
		DisplayName: "Amazon EKS Cluster Admin Policy",
		Description: "Grants administrator access to a cluster.",
	})
	assert.NoError(t, err)

	// Test entitlements
	entitlements, _, _, err := builder.Entitlements(t.Context(), resource, nil)
	assert.NoError(t, err)
	assert.Len(t, entitlements, 1)
	assert.Equal(t, "access_policy:arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy:assigned", entitlements[0].Id)
}

func TestPolicyBuilder_PolicyResource(t *testing.T) {
	// Create a mock EKS client
	eksClient := &client.EKSClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Test policy resource creation
	policy := &client.AccessPolicy{
		PolicyARN: "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
		AccessScope: client.AccessScope{
			Type: "cluster",
		},
		DisplayName: "Amazon EKS Cluster Admin Policy",
		Description: "Grants administrator access to a cluster.",
	}

	resource, err := builder.policyResource(policy)
	assert.NoError(t, err)

	assert.Equal(t, policy.PolicyARN, resource.Id.Resource)
	assert.Equal(t, "access_policy", resource.Id.ResourceType)
	assert.Equal(t, policy.DisplayName, resource.DisplayName)
}

func TestPolicyBuilder_GetStandardPolicies(t *testing.T) {
	// Create a mock EKS client
	eksClient := &client.EKSClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Test getStandardPolicies
	policies := builder.getStandardPolicies()

	// Should have 5 standard policies
	assert.Len(t, policies, 5)

	// Check that each policy has the required fields
	for _, policy := range policies {
		assert.NotEmpty(t, policy.PolicyARN)
		assert.NotEmpty(t, policy.DisplayName)
		assert.NotEmpty(t, policy.Description)
	}

	// Check specific policies
	clusterAdminPolicy := policies[0]
	assert.Equal(t, "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy", clusterAdminPolicy.PolicyARN)
	assert.Equal(t, "AmazonEKSClusterAdminPolicy", clusterAdminPolicy.DisplayName)
	assert.Contains(t, clusterAdminPolicy.Description, "administrator access")

	// Check that we have the AmazonEKSAdminViewPolicy (added in April 2024)
	adminViewPolicy := policies[4]
	assert.Equal(t, "arn:aws:eks::aws:cluster-access-policy/AmazonEKSAdminViewPolicy", adminViewPolicy.PolicyARN)
	assert.Equal(t, "AmazonEKSAdminViewPolicy", adminViewPolicy.DisplayName)
}
