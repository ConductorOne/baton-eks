package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/conductorone/baton-eks/pkg/client"
	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	"go.uber.org/zap"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

// iamRoleBuilder syncs AWS IAM Roles as Baton resources.
type iamRoleBuilder struct {
	eksClient    *client.EKSClient
	resourceType *v2.ResourceType
}

// ResourceType returns the resource type for IAM Roles.
func (i *iamRoleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return i.resourceType
}

// List fetches all IAM Roles from the AWS API.
func (i *iamRoleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	var rv []*v2.Resource

	bag, err := k8s.ParsePageToken(pToken.Token)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to parse page token: %w", err)
	}

	var iamNextToken *string
	if bag.PageToken() != "" {
		iamNextToken = aws.String(bag.PageToken())
	}

	roles, nextPageToken, err := i.eksClient.ListIAMRoles(ctx, iamNextToken)
	if err != nil {
		l.Error("failed to list IAM roles", zap.Error(err))
		return nil, "", nil, fmt.Errorf("failed to list IAM roles: %w", err)
	}

	for _, role := range roles {
		resource, err := i.roleResource(role)
		if err != nil {
			l.Error("failed to create role resource",
				zap.String("role_name", role.RoleName),
				zap.Error(err))
			continue
		}
		rv = append(rv, resource)
	}

	var nextPageTokenStr string
	if nextPageToken != nil && *nextPageToken != "" {
		bag.Push(pagination.PageState{Token: *nextPageToken})
		token, err := bag.Marshal()
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to marshal pagination bag: %w", err)
		}
		nextPageTokenStr = token
	}

	return rv, nextPageTokenStr, nil, nil
}

// roleResource creates a Baton resource from an AWS IAM Role.
func (i *iamRoleBuilder) roleResource(role *client.IAMRole) (*v2.Resource, error) {
	// Prepare profile with role metadata
	profile := map[string]interface{}{
		"role_name": role.RoleName,
		"role_id":   role.RoleID,
		"arn":       role.ARN,
	}

	// Add optional fields if they exist
	if role.Path != nil {
		profile["path"] = *role.Path
	}
	if role.CreateDate != nil {
		profile["create_date"] = role.CreateDate.Format("2006-01-02T15:04:05Z")
	}

	// Create resource as a role
	resource, err := rs.NewRoleResource(
		role.RoleName,
		ResourceTypeIAMRole,
		role.ARN, // Use ARN as the resource ID
		[]rs.RoleTraitOption{rs.WithRoleProfile(profile)},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create role resource: %w", err)
	}

	return resource, nil
}

// Entitlements returns entitlements for IAM Role resources.
func (i *iamRoleBuilder) Entitlements(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	assigmentEnt := entitlement.NewAssignmentEntitlement(
		resource,
		"assignment",
		entitlement.WithDisplayName(fmt.Sprintf("%s Role", resource.DisplayName)),
		entitlement.WithDescription(fmt.Sprintf("Can assume the %s role in AWS", resource.DisplayName)),
		entitlement.WithGrantableTo(
			ResourceTypeIAMUser,
		),
	)

	return []*v2.Entitlement{assigmentEnt}, "", nil, nil
}

// Grants returns permission grants for IAM Role resources.
func (i *iamRoleBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	var rv []*v2.Grant

	// Extract role ARN from resource
	if resource.Id == nil || resource.Id.Resource == "" {
		return nil, "", nil, fmt.Errorf("invalid resource ID")
	}
	roleARN := resource.Id.Resource

	// Extract role name from ARN
	// ARN format: arn:aws:iam::account:role/role-name
	parts := strings.Split(roleARN, "/")
	if len(parts) < 2 {
		l.Error("invalid role ARN format", zap.String("role_arn", roleARN))
		return nil, "", nil, fmt.Errorf("invalid role ARN format: %s", roleARN)
	}
	roleName := parts[len(parts)-1]

	// Get principals that can assume this role
	principals, err := i.eksClient.GetIAMRoleTrustPrincipals(ctx, roleName)
	if err != nil {
		l.Error("failed to get role trust principals",
			zap.String("role_name", roleName),
			zap.Error(err))
		// Don't fail completely, just log and continue
		return rv, "", nil, nil
	}

	// Create grants for principals that can assume this role
	for _, principal := range principals {
		// Skip wildcard principals and service principals for now
		if principal == "*" || strings.Contains(principal, ":service/") {
			continue
		}

		// Only handle IAM user principals
		if strings.Contains(principal, ":user/") {
			principalResource := k8s.GenerateResourceForGrant(principal, ResourceTypeIAMUser.Id)
			g := grant.NewGrant(
				resource,
				"assignment",
				principalResource,
			)
			rv = append(rv, g)

			l.Debug("created role assume grant",
				zap.String("role_arn", roleARN),
				zap.String("principal_arn", principal))
		}
	}

	return rv, "", nil, nil
}

// NewIAMRoleBuilder creates a new IAM role builder.
func NewIAMRoleBuilder(eksClient *client.EKSClient) *iamRoleBuilder {
	return &iamRoleBuilder{
		eksClient:    eksClient,
		resourceType: ResourceTypeIAMRole,
	}
}
