package connector

import (
	"testing"

	"github.com/conductorone/baton-eks/pkg/client"
	"github.com/stretchr/testify/assert"
)

func TestIAMRoleBuilder_ResourceType(t *testing.T) {
	// Create a nil client for testing (we only need the resource type)
	var eksClient *client.EKSClient

	// Create IAM role builder
	builder := NewIAMRoleBuilder(eksClient)

	// Test resource type
	resourceType := builder.ResourceType(t.Context())

	assert.NotNil(t, resourceType)
	assert.Equal(t, "iam_role", resourceType.Id)
	assert.Equal(t, "IAM Role", resourceType.DisplayName)
	assert.Equal(t, "AWS IAM Role", resourceType.Description)
}

func TestIAMRoleBuilder_RoleResource(t *testing.T) {
	// Create a nil client for testing (we only need the resource type)
	var eksClient *client.EKSClient

	// Create IAM role builder
	builder := NewIAMRoleBuilder(eksClient)

	// Create a test IAM role
	testRole := &client.IAMRole{
		RoleName: "test-role",
		RoleID:   "AIDACKCEVSQ6C2EXAMPLE",
		ARN:      "arn:aws:iam::123456789012:role/test-role",
	}

	// Test role resource creation
	resource, err := builder.roleResource(testRole)

	assert.NoError(t, err)
	assert.NotNil(t, resource)
	assert.Equal(t, "test-role", resource.DisplayName)
	assert.Equal(t, "arn:aws:iam::123456789012:role/test-role", resource.Id.Resource)
	assert.Equal(t, "iam_role", resource.Id.ResourceType)
}
