package connector

import (
	"context"
	"testing"

	eksTypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/conductorone/baton-eks/pkg/client"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockAccessPolicyClient implements the AccessPolicyClient interface for testing.
type mockAccessPolicyClient struct{}

func (m *mockAccessPolicyClient) ListNamespaces(ctx context.Context, opts metav1.ListOptions) (*corev1.NamespaceList, error) {
	return &corev1.NamespaceList{
		Items: []corev1.Namespace{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
			},
		},
	}, nil
}

func (m *mockAccessPolicyClient) GetAccessEntriesWithPolicy(ctx context.Context, policyARN string, nextToken *string) ([]string, *string, error) {
	return []string{"arn:aws:iam::123456789012:user/testuser"}, nil, nil
}

func (m *mockAccessPolicyClient) GetAssociatedAccessPolicies(ctx context.Context, principalARN string) ([]eksTypes.AssociatedAccessPolicy, error) {
	return []eksTypes.AssociatedAccessPolicy{}, nil
}

func (m *mockAccessPolicyClient) CreateAccessEntry(ctx context.Context, principalARN string) (*eksTypes.AccessEntry, error) {
	return &eksTypes.AccessEntry{
		PrincipalArn: &principalARN,
	}, nil
}

func (m *mockAccessPolicyClient) AssociateAccessPolicy(ctx context.Context, principalARN string, policyARN string, accessScope *eksTypes.AccessScope) error {
	return nil
}

func (m *mockAccessPolicyClient) DisassociateAccessPolicy(ctx context.Context, principalARN string, policyARN string) error {
	return nil
}

func TestPolicyBuilder_ResourceType(t *testing.T) {
	// Create a mock EKS client
	eksClient := &mockAccessPolicyClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Test resource type
	resourceType := builder.ResourceType(context.Background())
	assert.Equal(t, "access_policy", resourceType.Id)
	assert.Equal(t, "Access Policy", resourceType.DisplayName)
	assert.Equal(t, "EKS Access Policy", resourceType.Description)
}

func TestPolicyBuilder_Entitlements(t *testing.T) {
	// Create a mock EKS client
	eksClient := &mockAccessPolicyClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Create a test resource
	resource, err := builder.policyResource(&client.AccessPolicy{
		PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
		DisplayName: "Amazon EKS Cluster Admin Policy",
		Description: "Grants administrator access to a cluster.",
	})
	assert.NoError(t, err)

	// Test entitlements
	entitlements, _, _, err := builder.Entitlements(context.Background(), resource, nil)
	assert.NoError(t, err)
	assert.Len(t, entitlements, 2) // One for namespace + one for cluster
	// Check that we have the correct entitlement IDs (which include resource info)
	entitlementIDs := make([]string, len(entitlements))
	for i, ent := range entitlements {
		entitlementIDs[i] = ent.Id
	}

	// The actual entitlement IDs are in the format: resource_type:resource_id:entitlement_slug
	expectedDefaultId := "access_policy:arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy:assigned:default"
	expectedClusterId := "access_policy:arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy:assigned:cluster"

	assert.Contains(t, entitlementIDs, expectedDefaultId)
	assert.Contains(t, entitlementIDs, expectedClusterId)
}

func TestPolicyBuilder_PolicyResource(t *testing.T) {
	// Create a mock EKS client
	eksClient := &mockAccessPolicyClient{}

	// Create policy builder
	builder := NewAccessPolicyBuilder(eksClient)

	// Test policy resource creation
	policy := &client.AccessPolicy{
		PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
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
	eksClient := &mockAccessPolicyClient{}

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
