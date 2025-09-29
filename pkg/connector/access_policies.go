package connector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	eksTypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/conductorone/baton-eks/pkg/client"
	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	eksClient    client.AccessPolicyClient
	resourceType *v2.ResourceType
}

// ResourceType returns the resource type for Access Policies.
func (a *accessPolicyBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return a.resourceType
}

// List fetches all Access Policies from the EKS API.
func (a *accessPolicyBuilder) List(ctx context.Context, _ *v2.ResourceId, _ *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	// Initialize empty resource slice.
	var rv []*v2.Resource

	// Get all standard EKS Access Policies
	policies := a.getStandardPolicies()

	// Process policies for current page
	for _, policy := range policies {
		resource, err := a.policyResource(policy)
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
func (a *accessPolicyBuilder) policyResource(policy *client.AccessPolicy) (*v2.Resource, error) {
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
func (a *accessPolicyBuilder) Entitlements(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var entitlements []*v2.Entitlement
	var opts metav1.ListOptions
	// Parse pagination token.
	var tokenStr string
	if pToken != nil {
		tokenStr = pToken.Token
	}
	bag, err := k8s.ParsePageToken(tokenStr)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to parse page token: %w", err)
	}
	opts.Continue = bag.PageToken()
	namespaces, err := a.eksClient.ListNamespaces(ctx, opts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	for _, ns := range namespaces.Items {
		entitlementName := fmt.Sprintf("assigned:%s", ns.Name)
		nsEnt := entitlement.NewAssignmentEntitlement(
			resource,
			entitlementName,
			entitlement.WithDisplayName(fmt.Sprintf("%s Access policy scoped to %s namespace", resource.DisplayName, ns.Name)),
			entitlement.WithDescription(fmt.Sprintf("Grants assignmet to the %s Access policy in %s namespace", resource.DisplayName, ns.Name)),
			entitlement.WithGrantableTo(
				ResourceTypeIAMUser,
				ResourceTypeIAMRole,
			),
		)
		entitlements = append(entitlements, nsEnt)
	}

	if namespaces.Continue != "" {
		nextPageToken, err := k8s.HandleKubePagination(&namespaces.ListMeta, bag)
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to handle pagination: %w", err)
		}
		return entitlements, nextPageToken, nil, nil
	}

	assignedEnt := entitlement.NewAssignmentEntitlement(
		resource,
		"assigned:cluster",
		entitlement.WithDisplayName(fmt.Sprintf("%s Access policy cluster scoped", resource.DisplayName)),
		entitlement.WithDescription(fmt.Sprintf("Grants assignmet to the %s Access policy for cluster", resource.DisplayName)),
		entitlement.WithGrantableTo(
			ResourceTypeIAMUser,
			ResourceTypeIAMRole,
		),
	)
	entitlements = append(entitlements, assignedEnt)

	return entitlements, "", nil, nil
}

// Grants returns permission grants for Access Policy resources.
func (a *accessPolicyBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
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

	principalARNs, eksNextPageToken, err := a.eksClient.GetAccessEntriesWithPolicy(ctx, policyARN, eksNextToken)
	if err != nil {
		l.Error("failed to get access entries", zap.Error(err))
		return nil, "", nil, fmt.Errorf("failed to get access entries: %w", err)
	}

	// Create grants for principals in this page
	for _, principalARN := range principalARNs {
		// Get the scope information for this specific policy
		policyScope, err := a.getPolicyScope(ctx, principalARN, policyARN)
		if err != nil {
			l.Error("failed to get policy scope", zap.Error(err))
			continue // Skip this principal if we can't get scope info
		}
		if policyScope == nil {
			// Policy is not associated to the principal
			continue
		}

		// Create grants based on scope
		grants := a.createGrantsForPrincipal(resource, principalARN, policyScope)
		rv = append(rv, grants...)
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

// getPolicyScope retrieves the scope information for a specific policy.
func (a *accessPolicyBuilder) getPolicyScope(ctx context.Context, principalARN, policyARN string) (*eksTypes.AccessScope, error) {
	associatedPolicies, err := a.eksClient.GetAssociatedAccessPolicies(ctx, principalARN)
	if err != nil {
		return nil, fmt.Errorf("failed to get associated access policies: %w", err)
	}

	// Find the specific policy we're looking for
	for _, policy := range associatedPolicies {
		if *policy.PolicyArn == policyARN {
			return policy.AccessScope, nil
		}
	}

	return nil, nil
}

// createGrantsForPrincipal creates grants based on the policy scope.
func (a *accessPolicyBuilder) createGrantsForPrincipal(resource *v2.Resource, principalARN string, scope *eksTypes.AccessScope) []*v2.Grant {
	var grants []*v2.Grant
	l := ctxzap.Extract(context.Background())

	// Determine principal type and resource type
	var principalResource *v2.Resource
	var resourceType string
	if strings.Contains(principalARN, ":role/") {
		principalResource = k8s.GenerateResourceForGrant(principalARN, ResourceTypeIAMRole.Id)
		resourceType = "role"
	} else {
		principalResource = k8s.GenerateResourceForGrant(principalARN, ResourceTypeIAMUser.Id)
		resourceType = "user"
	}

	// Create grants based on scope
	if scope.Type == "namespace" && len(scope.Namespaces) > 0 {
		// Create a grant for each namespace
		for _, namespace := range scope.Namespaces {
			entitlementName := fmt.Sprintf("assigned:%s", namespace)
			grant := a.createGrant(resource, principalResource, entitlementName, principalARN, resourceType)
			grants = append(grants, grant)
		}
	} else {
		// Cluster scope - single grant
		grant := a.createGrant(resource, principalResource, "assigned:cluster", principalARN, resourceType)
		grants = append(grants, grant)
	}

	l.Debug("created policy grants",
		zap.String("policy_arn", resource.Id.Resource),
		zap.String("principal_arn", principalARN),
		zap.String("scope_type", string(scope.Type)),
		zap.Strings("namespaces", scope.Namespaces),
		zap.Int("grant_count", len(grants)))

	return grants
}

// createGrant creates a single grant with appropriate options.
func (a *accessPolicyBuilder) createGrant(
	resource *v2.Resource,
	principalResource *v2.Resource,
	entitlementName string,
	principalARN string,
	resourceType string,
) *v2.Grant {
	grantOpts := []grant.GrantOption{}

	// Add expandable options for roles
	if resourceType == "role" {
		grantExpandable := &v2.GrantExpandable{
			EntitlementIds: []string{
				fmt.Sprintf("role:%s:assignment", principalARN),
			},
		}
		grantOpts = append(grantOpts, grant.WithAnnotation(grantExpandable))
	}

	return grant.NewGrant(
		resource,
		entitlementName,
		principalResource,
		grantOpts...,
	)
}

// parseEntitlementScope parses the scope from an entitlement name.
func (a *accessPolicyBuilder) parseEntitlementScope(entitlementName string) *eksTypes.AccessScope {
	// Entitlement names are in the format "access_policy:policy_arn:assigned:scope"
	// where scope can be "cluster" or a namespace name
	assignedIndex := strings.Index(entitlementName, ":assigned:")
	if assignedIndex == -1 {
		// Fallback to cluster scope if ":assigned:" not found
		return &eksTypes.AccessScope{Type: eksTypes.AccessScopeTypeCluster}
	}

	// Extract everything after ":assigned:"
	scopeValue := entitlementName[assignedIndex+len(":assigned:"):]

	if scopeValue == "cluster" {
		return &eksTypes.AccessScope{Type: eksTypes.AccessScopeTypeCluster}
	} else {
		// It's a namespace scope
		return &eksTypes.AccessScope{
			Type:       eksTypes.AccessScopeTypeNamespace,
			Namespaces: []string{scopeValue},
		}
	}
}

func (a *accessPolicyBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	if principal.Id.ResourceType != ResourceTypeIAMUser.Id {
		return nil, fmt.Errorf("principal is not an IAM user")
	}

	principalARN := principal.Id.Resource
	policyARN := entitlement.Resource.Id.Resource

	// Parse the scope from the entitlement name
	accessScope := a.parseEntitlementScope(entitlement.Id)

	// Create access entry if it does not exist
	_, err := a.eksClient.CreateAccessEntry(ctx, principalARN)
	if err != nil {
		if !isAccessEntryAlreadyExistsError(err) {
			return nil, fmt.Errorf("failed to create access entry: %w", err)
		}
	}

	// If scope is namespace, we fetch the policy associated and update it adding the namespace,
	// otherwise we would be disassociating the policy from existing namespaces.
	if accessScope.Type == eksTypes.AccessScopeTypeNamespace {
		policyScope, err := a.getPolicyScope(ctx, principalARN, policyARN)
		if err != nil {
			l.Error("failed to get policy scope", zap.Error(err))
			return nil, fmt.Errorf("failed to get policy scope: %w", err)
		}
		if policyScope != nil {
			if policyScope.Type == eksTypes.AccessScopeTypeCluster {
				// Trying to grant a namespace scoped policy, but user already has a cluster scoped policy.
				// Scoping the policy to the namespace would disassociate the policy from the cluster, affecting other namespaces.
				return nil, fmt.Errorf("try to grant a namespace scoped policy, but user already has a cluster scoped policy")
			}
			if policyScope.Type == eksTypes.AccessScopeTypeNamespace {
				// Verify if the namespace is already in the policy scope
				for _, namespace := range policyScope.Namespaces {
					if namespace == accessScope.Namespaces[0] {
						// Namespace already in policy scope, no need to associate again.
						return annotations.New(&v2.GrantAlreadyExists{}), nil
					}
				}
				// Namespace not in policy scope, we need to associate the policy with the new namespace
				accessScope.Namespaces = append(accessScope.Namespaces, policyScope.Namespaces...)
			}
		}
	}
	// Associate the policy with the specified scope
	err = a.eksClient.AssociateAccessPolicy(ctx, principalARN, policyARN, accessScope)
	if err != nil {
		return nil, fmt.Errorf("failed to create policy association: %w", err)
	}

	return nil, nil
}

func isAccessEntryAlreadyExistsError(err error) bool {
	var resourceInUseErr *eksTypes.ResourceInUseException
	var conflictErr *eksTypes.ClientException

	// Use errors.As to check for specific AWS SDK v2 error types
	return strings.Contains(err.Error(), "already exists") ||
		errors.As(err, &resourceInUseErr) ||
		errors.As(err, &conflictErr)
}

func (a *accessPolicyBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	if grant.Principal.Id.ResourceType != ResourceTypeIAMUser.Id {
		return nil, fmt.Errorf("principal is not an IAM user")
	}

	policyARN := grant.Entitlement.Resource.Id.Resource
	principalARN := grant.Principal.Id.Resource

	accessScope := a.parseEntitlementScope(grant.Entitlement.Id)
	if accessScope.Type == eksTypes.AccessScopeTypeNamespace {
		policyScope, err := a.getPolicyScope(ctx, principalARN, policyARN)
		if err != nil {
			l.Error("failed to get policy scope", zap.Error(err))
			return nil, fmt.Errorf("failed to get policy scope: %w", err)
		}
		if policyScope == nil {
			// Policy is not associated to the principal
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}
		if policyScope.Type == eksTypes.AccessScopeTypeCluster {
			// Trying to revoke a namespace scoped policy, but user already has a cluster scoped policy.
			// Revoking the policy from the namespace would disassociate the policy from the cluster, affecting other namespaces.
			return nil, fmt.Errorf("trying to revoke a namespace scoped policy, but user already has a cluster scoped policy")
		}
		// Type == Namespace
		containsNamespace := false
		for _, namespace := range policyScope.Namespaces {
			if namespace == accessScope.Namespaces[0] {
				containsNamespace = true
				break
			}
		}
		if !containsNamespace {
			return annotations.New(&v2.GrantAlreadyRevoked{}), nil
		}
		if len(policyScope.Namespaces) > 1 {
			// Filter out the namespace to be revoked
			var remainingNamespaces []string
			for _, namespace := range policyScope.Namespaces {
				if namespace != accessScope.Namespaces[0] {
					remainingNamespaces = append(remainingNamespaces, namespace)
				}
			}
			// Update the policy association with remaining namespaces
			updatedScope := &eksTypes.AccessScope{
				Type:       eksTypes.AccessScopeTypeNamespace,
				Namespaces: remainingNamespaces,
			}
			err = a.eksClient.AssociateAccessPolicy(ctx, principalARN, policyARN, updatedScope)
			if err != nil {
				return nil, fmt.Errorf("failed to update policy association: %w", err)
			}
			return nil, nil
		}
	}

	err := a.eksClient.DisassociateAccessPolicy(ctx, principalARN, policyARN)
	if err != nil {
		l.Error("failed to disassociate access policy", zap.Error(err))
		return nil, err
	}
	return nil, nil
}

// getStandardPolicies returns all standard EKS Access Policies.
// Access policies are defined by EKS and cannot be created or modified by the user.
// https://docs.aws.amazon.com/eks/latest/userguide/access-policy-permissions.html.
func (a *accessPolicyBuilder) getStandardPolicies() []*client.AccessPolicy {
	// The following list is limited to policies that are related (assignable) to users/roles.
	return []*client.AccessPolicy{
		{
			PolicyARN:   "arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
			DisplayName: "AmazonEKSClusterAdminPolicy",
			Description: "This access policy includes permissions that grant an IAM principal administrator access to a cluster. " +
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
func NewAccessPolicyBuilder(eksClient client.AccessPolicyClient) *accessPolicyBuilder {
	return &accessPolicyBuilder{
		eksClient:    eksClient,
		resourceType: ResourceTypeAccessPolicy,
	}
}
