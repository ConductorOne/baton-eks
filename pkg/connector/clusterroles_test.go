package connector

import (
	"context"
	"fmt"
	"testing"
	"time"

	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/stretchr/testify/assert"
)

// MockClusterRoleBindingProvider implements k8s.ClusterRoleBindingProvider for testing.
type MockClusterRoleBindingProvider struct {
	roleBindings    []rbacv1.RoleBinding
	clusterBindings []rbacv1.ClusterRoleBinding
	shouldReturnErr bool
}

func (m *MockClusterRoleBindingProvider) GetMatchingBindingsForClusterRole(ctx context.Context, clusterRoleName string) ([]rbacv1.RoleBinding, []rbacv1.ClusterRoleBinding, error) {
	if m.shouldReturnErr {
		return nil, nil, assert.AnError
	}
	return m.roleBindings, m.clusterBindings, nil
}

// MockEKSClient implements client.EKSClient for testing.
type MockEKSClient struct {
	usersByGroup    map[string][]string
	usersByUsername map[string][]string
	shouldReturnErr bool
}

func (m *MockEKSClient) LookupArnsByGroup(ctx context.Context, groupName string) ([]string, error) {
	if m.shouldReturnErr {
		return nil, assert.AnError
	}
	if users, exists := m.usersByGroup[groupName]; exists {
		return users, nil
	}
	return []string{}, nil
}

func (m *MockEKSClient) LookupArnsByUsername(ctx context.Context, username string) ([]string, error) {
	if m.shouldReturnErr {
		return nil, assert.AnError
	}
	if users, exists := m.usersByUsername[username]; exists {
		return users, nil
	}
	return []string{}, nil
}

func TestClusterRoleResource(t *testing.T) {
	tests := []struct {
		name        string
		clusterRole *rbacv1.ClusterRole
		wantErr     bool
	}{
		{
			name: "basic cluster role",
			clusterRole: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-role",
					UID:               "test-uid",
					CreationTimestamp: metav1.Time{Time: time.Now()},
					Labels:            map[string]string{"app": "test"},
					Annotations:       map[string]string{"description": "test role"},
				},
			},
			wantErr: false,
		},
		{
			name: "cluster role with aggregation rule",
			clusterRole: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "aggregated-role",
					UID:               "agg-uid",
					CreationTimestamp: metav1.Time{Time: time.Now()},
				},
				AggregationRule: &rbacv1.AggregationRule{
					ClusterRoleSelectors: []metav1.LabelSelector{
						{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, err := clusterRoleResource(tt.clusterRole)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, resource)
			assert.Equal(t, tt.clusterRole.Name, resource.Id.Resource)
			assert.Equal(t, k8s.ResourceTypeClusterRole.Id, resource.Id.ResourceType)
		})
	}
}

func TestClusterRoleBuilder_ResourceType(t *testing.T) {
	mockBindingProvider := &MockClusterRoleBindingProvider{}

	builder := NewClusterRoleBuilder(nil, mockBindingProvider, nil)

	ctx := context.Background()
	resourceType := builder.ResourceType(ctx)

	assert.Equal(t, k8s.ResourceTypeClusterRole, resourceType)
}

func TestProcessGrants(t *testing.T) {
	// Test the processGrants helper function
	resource := &v2.Resource{
		Id: &v2.ResourceId{
			ResourceType: k8s.ResourceTypeClusterRole.Id,
			Resource:     "test-role",
		},
		DisplayName: "Test Role",
	}

	tests := []struct {
		name            string
		users           []string
		entitlementName string
		expectedCount   int
	}{
		{
			name:            "single user",
			users:           []string{"user1"},
			entitlementName: "test:member",
			expectedCount:   1,
		},
		{
			name:            "multiple users",
			users:           []string{"user1", "user2", "user3"},
			entitlementName: "test:member",
			expectedCount:   3,
		},
		{
			name:            "no users",
			users:           []string{},
			entitlementName: "test:member",
			expectedCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grants := processGrants(tt.users, resource, tt.entitlementName)
			assert.Len(t, grants, tt.expectedCount)

			for i, grant := range grants {
				// The grant should have the cluster role as the entitlement resource and the user as the principal
				assert.Equal(t, resource, grant.Entitlement.Resource)
				expectedEntitlementID := fmt.Sprintf("cluster_role:test-role:%s", tt.entitlementName)
				assert.Equal(t, expectedEntitlementID, grant.Entitlement.Id)
				// The principal should be a user resource
				assert.Equal(t, ResourceTypeIAMUser.Id, grant.Principal.Id.ResourceType)
				assert.Equal(t, tt.users[i], grant.Principal.Id.Resource)
			}
		})
	}
}
