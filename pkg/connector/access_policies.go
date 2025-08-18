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

// accessPolicyBuilder syncs EKS Access Policies as Baton resources.
type accessPolicyBuilder struct {
	eksClient    *client.EKSClient
	resourceType *v2.ResourceType
}

// ResourceType returns the resource type for Access Policies.
func (p *accessPolicyBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return p.resourceType
}

// List fetches all Access Policies from the EKS API.
func (p *accessPolicyBuilder) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	// Initialize empty resource slice.
	var rv []*v2.Resource

	// Get all standard EKS Access Policies
	policies := p.getStandardPolicies()

	// Process policies for current page
	for _, policy := range policies {
		resource, err := p.policyResource(policy)
		if err != nil {
			l.Error("failed to create policy resource",
				zap.String("policy_arn", policy.PolicyARN),
				zap.Error(err))
			continue
		}
		rv = append(rv, resource)
	}

	return rv, "", nil, nil
}

// policyResource creates a Baton resource from an EKS Access Policy.
func (p *accessPolicyBuilder) policyResource(policy *client.AccessPolicy) (*v2.Resource, error) {
	// Prepare profile with policy metadata
	profile := map[string]interface{}{
		"display_name": policy.DisplayName,
		"policy_arn":   policy.PolicyARN,
		"description":  policy.Description,
	}

	// Create resource as a role - pass the policy ARN directly as the raw ID
	resource, err := rs.NewRoleResource(
		policy.DisplayName,
		ResourceTypeAccessPolicy,
		policy.PolicyARN, // Pass the policy ARN directly as the object ID
		[]rs.RoleTraitOption{rs.WithRoleProfile(profile)},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy resource: %w", err)
	}

	return resource, nil
}

// Entitlements returns entitlements for Access Policy resources.
func (p *accessPolicyBuilder) Entitlements(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	assignedEnt := entitlement.NewAssignmentEntitlement(
		resource,
		"assigned",
		entitlement.WithDisplayName(fmt.Sprintf("%s Access policy", resource.DisplayName)),
		entitlement.WithDescription(fmt.Sprintf("Grants assignmet to the %s Access policy", resource.DisplayName)),
		entitlement.WithGrantableTo(
			k8s.ResourceTypeServiceAccount,
			k8s.ResourceTypeGroup,
			ResourceTypeIAMUser,
			ResourceTypeIAMRole,
		),
	)

	return []*v2.Entitlement{assignedEnt}, "", nil, nil
}

// Grants returns permission grants for Access Policy resources.
func (p *accessPolicyBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	var rv []*v2.Grant

	policyARN := resource.Id.Resource
	bag, err := k8s.ParsePageToken(pToken.Token)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to parse page token: %w", err)
	}

	var eksNextToken *string
	if bag.PageToken() != "" {
		eksNextToken = aws.String(bag.PageToken())
	}

	principalARNs, eksNextPageToken, err := p.eksClient.GetAccessEntriesWithPolicy(ctx, policyARN, eksNextToken)
	if err != nil {
		l.Error("failed to get access entries", zap.Error(err))
		return nil, "", nil, fmt.Errorf("failed to get access entries: %w", err)
	}

	// Create grants for principals in this page
	for _, principalARN := range principalARNs {
		resourceType := ResourceTypeIAMUser
		grantOpts := []grant.GrantOption{}

		// Create grant for this principal
		if strings.Contains(principalARN, ":role/") {
			resourceType = ResourceTypeIAMRole
			principalResource := k8s.GenerateResourceForGrant(principalARN, ResourceTypeIAMRole.Id)
			grantExpandable := &v2.GrantExpandable{
				EntitlementIds: []string{
					fmt.Sprintf("iam_role:%s:assumes", principalARN),
				},
			}
			grantOpts = append(grantOpts, grant.WithAnnotation(grantExpandable))
			g := grant.NewGrant(
				resource,
				"assigned",
				principalResource,
				grantOpts...,
			)
			rv = append(rv, g)

			l.Debug("created policy grant to role",
				zap.String("policy_arn", policyARN),
				zap.String("principal_arn", principalARN))
		} else {
			// Handle IAM user principals
			principalResource := k8s.GenerateResourceForGrant(principalARN, resourceType.Id)
			g := grant.NewGrant(
				resource,
				"assigned",
				principalResource,
				grantOpts...,
			)
			rv = append(rv, g)

			l.Debug("created policy grant to user",
				zap.String("policy_arn", policyARN),
				zap.String("principal_arn", principalARN))
		}
	}

	var nextPageToken string
	if eksNextPageToken != nil && *eksNextPageToken != "" {
		bag.Push(pagination.PageState{Token: *eksNextPageToken})
		token, err := bag.Marshal()
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to marshal pagination bag: %w", err)
		}
		nextPageToken = token
	}

	return rv, nextPageToken, nil, nil
}

// getStandardPolicies returns all standard EKS Access Policies.
// Access policies are defined by EKS and cannot be created or modified by the user.
// https://docs.aws.amazon.com/eks/latest/userguide/access-policy-permissions.html.
func (p *accessPolicyBuilder) getStandardPolicies() []*client.AccessPolicy {
	// The following list is limited to policies that are related (assignable) to users/roles.
	return []*client.AccessPolicy{
		{
			PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
			DisplayName: "AmazonEKSClusterAdminPolicy",
			Description: "his access policy includes permissions that grant an IAM principal administrator access to a cluster. " +
				"When associated to an access entry, its access scope is typically the cluster, rather than a Kubernetes namespace. " +
				"If you want an IAM principal to have a more limited administrative scope, consider associating the AmazonEKSAdminPolicy access policy to your access entry instead.",
		},
		{
			PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSAdminPolicy",
			DisplayName: "AmazonEKSAdminPolicy",
			Description: "This access policy includes permissions that grant an IAM principal most permissions to resources. " +
				"When associated to an access entry, its access scope is typically one or more Kubernetes namespaces. " +
				"If you want an IAM principal to have administrator access to all resources on your cluster, associate the AmazonEKSClusterAdminPolicy access policy to your access entry instead.",
		},
		{
			PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy",
			DisplayName: "AmazonEKSViewPolicy",
			Description: "This access policy includes permissions that allow an IAM principal to view most Kubernetes resources.",
		},
		{
			PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSEditPolicy",
			DisplayName: "AmazonEKSEditPolicy",
			Description: "This access policy includes permissions that allow an IAM principal to edit most Kubernetes resources.",
		},
		{
			PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSAdminViewPolicy",
			DisplayName: "AmazonEKSAdminViewPolicy",
			Description: "This access policy includes permissions that grant an IAM principal access to list/view all resources in a cluster. Note this includes Kubernetes Secrets.",
		},
	}
}

// NewPolicyBuilder creates a new policy builder.
func NewAccessPolicyBuilder(eksClient *client.EKSClient) *accessPolicyBuilder {
	return &accessPolicyBuilder{
		eksClient:    eksClient,
		resourceType: ResourceTypeAccessPolicy,
	}
}
