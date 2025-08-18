package connector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/conductorone/baton-eks/pkg/client"
	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const cacheTTL = 5 * time.Minute
const clusterScopedMember = "all:member"

// clusterRoleBuilder syncs Kubernetes ClusterRoles as Baton resources.
type clusterRoleBuilder struct {
	client          kubernetes.Interface
	bindingProvider k8s.ClusterRoleBindingProvider
	// Cached namespaces
	cachedNamespaces     []string
	nsMutex              sync.Mutex
	nsCacheExpiry        time.Time
	userMappingsProvider *client.EKSClient
}

// ResourceType returns the resource type for ClusterRole.
func (c *clusterRoleBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return k8s.ResourceTypeClusterRole
}

// List fetches all ClusterRoles from the Kubernetes API.
func (c *clusterRoleBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	// Initialize empty resource slice.
	var rv []*v2.Resource

	// Parse pagination token.
	bag, err := k8s.ParsePageToken(pToken.Token)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to parse page token: %w", err)
	}

	// Set up list options with pagination.
	opts := metav1.ListOptions{
		Limit:    k8s.ResourcesPageSize,
		Continue: bag.PageToken(),
	}

	// Fetch cluster roles from the Kubernetes API.
	l.Debug("fetching cluster roles", zap.String("continue_token", opts.Continue))
	resp, err := c.client.RbacV1().ClusterRoles().List(ctx, opts)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to list cluster roles: %w", err)
	}

	// Process each cluster role into a Baton resource.
	for _, clusterRole := range resp.Items {
		resource, err := clusterRoleResource(&clusterRole)
		if err != nil {
			l.Error("failed to create cluster role resource",
				zap.String("name", clusterRole.Name),
				zap.Error(err))
			continue
		}
		rv = append(rv, resource)
	}

	// Calculate next page token.
	nextPageToken, err := k8s.HandleKubePagination(&resp.ListMeta, bag)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to handle pagination: %w", err)
	}

	return rv, nextPageToken, nil, nil
}

// clusterRoleResource creates a Baton resource from a Kubernetes ClusterRole.
func clusterRoleResource(clusterRole *rbacv1.ClusterRole) (*v2.Resource, error) {
	// Prepare profile with standard metadata.
	profile := map[string]interface{}{
		"name":              clusterRole.Name,
		"uid":               string(clusterRole.UID),
		"creationTimestamp": clusterRole.CreationTimestamp.String(),
		"labels":            k8s.StringMapToAnyMap(clusterRole.Labels),
		"annotations":       k8s.StringMapToAnyMap(clusterRole.Annotations),
	}

	// Add aggregation rule if present
	if clusterRole.AggregationRule != nil {
		agRule, err := k8s.ParseAggregationRule(clusterRole.AggregationRule)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal aggregation rule: %w", err)
		}
		profile["aggregationRule"] = agRule
	}

	// Create resource as a role - pass the name directly as the raw ID.
	resource, err := rs.NewRoleResource(
		clusterRole.Name,
		k8s.ResourceTypeClusterRole,
		clusterRole.Name, // Pass the name directly as the object ID.
		[]rs.RoleTraitOption{rs.WithRoleProfile(profile)},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster role resource: %w", err)
	}

	return resource, nil
}

// Entitlements returns entitlements for ClusterRole resources.
func (c *clusterRoleBuilder) Entitlements(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var entitlements []*v2.Entitlement

	// Create the 'member' entitlement for the cluster role.
	memberEnt := entitlement.NewAssignmentEntitlement(
		resource,
		clusterScopedMember,
		entitlement.WithDisplayName(fmt.Sprintf("%s Cluster Role Member", resource.DisplayName)),
		entitlement.WithDescription(fmt.Sprintf("Grants membership to the %s cluster role", resource.DisplayName)),
		entitlement.WithGrantableTo(
			k8s.ResourceTypeServiceAccount,
			k8s.ResourceTypeGroup,
			k8s.ResourceTypeUser,
		),
	)
	entitlements = append(entitlements, memberEnt)

	// Each ClusterRole can be granted in a RoleBinding, thus binding it to a namespace.
	// Create entitlements for each namespace.
	err := c.cacheNamespaces(ctx)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to cache namespaces: %w", err)
	}

	for _, ns := range c.cachedNamespaces {
		entitlementName := fmt.Sprintf("%s:%s", ns, "member")
		nsEnt := entitlement.NewAssignmentEntitlement(
			resource,
			entitlementName,
			entitlement.WithDisplayName(fmt.Sprintf("%s Cluster Role Member in %s namespace", resource.DisplayName, ns)),
			entitlement.WithDescription(fmt.Sprintf("Grants membership to the %s cluster role in namespace %s", resource.DisplayName, ns)),
			entitlement.WithGrantableTo(
				k8s.ResourceTypeServiceAccount,
				k8s.ResourceTypeGroup,
				k8s.ResourceTypeUser,
			),
		)
		entitlements = append(entitlements, nsEnt)
	}
	return entitlements, "", nil, nil
}

// Grants returns permission grants for ClusterRole resources.
func (c *clusterRoleBuilder) Grants(ctx context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	var rv []*v2.Grant

	// Extract cluster role name from resource.
	if resource.Id == nil || resource.Id.Resource == "" {
		return nil, "", nil, fmt.Errorf("invalid resource ID")
	}
	name := resource.Id.Resource

	// Get matching role bindings and cluster role bindings from the binding provider.
	matchingRoleBindings, matchingClusterBindings, err := c.bindingProvider.GetMatchingBindingsForClusterRole(ctx, name)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get matching bindings: %w", err)
	}

	// If there are no bindings, there are no grants.
	if len(matchingRoleBindings) == 0 && len(matchingClusterBindings) == 0 {
		l.Debug("no bindings found for cluster role", zap.String("name", name))
		return nil, "", nil, nil
	}

	// Process each matching cluster binding.
	for _, binding := range matchingClusterBindings {
		// Process each subject in the binding.
		for _, subject := range binding.Subjects {
			if subject.Kind == k8s.SubjectKindServiceAccount {
				// These are Cluster's local service accounts, not AWS.
				saName := fmt.Sprintf("%s/%s", subject.Namespace, subject.Name) // SA are always namespaced, even if they can have cluster roles bind to cluster level.
				saResource := k8s.GenerateResourceForGrant(saName, k8s.ResourceTypeServiceAccount.Id)
				g := grant.NewGrant(
					resource,
					clusterScopedMember,
					saResource,
				)
				rv = append(rv, g)
			} else if (subject.APIGroup == k8s.RBACAPIGroup || subject.APIGroup == k8s.RBACAPIGroupV1) &&
				!strings.Contains(subject.Name, "system:") {
				var matchingARNs []string
				switch subject.Kind {
				case k8s.SubjectKindGroup:
					matchingARNs, err = c.userMappingsProvider.LookupArnsByGroup(ctx, subject.Name)
					if err != nil {
						return nil, "", nil, fmt.Errorf("failed to lookup ARNs for group %s: %w", subject.Name, err)
					}
				case k8s.SubjectKindUser:
					matchingARNs, err = c.userMappingsProvider.LookupArnsByUsername(ctx, subject.Name)
					if err != nil {
						return nil, "", nil, fmt.Errorf("failed to lookup ARNs for user %s: %w", subject.Name, err)
					}
				}
				rv = append(rv, processGrants(matchingARNs, resource, clusterScopedMember)...)
			}
		}
	}

	// Process each matching role binding.
	for _, binding := range matchingRoleBindings {
		namespace := binding.Namespace
		// Process each subject in the binding.
		for _, subject := range binding.Subjects {
			entName := fmt.Sprintf("%s:%s", namespace, "member")
			if subject.Kind == k8s.SubjectKindServiceAccount {
				// These are Cluster's local service accounts, not AWS.
				saName := fmt.Sprintf("%s/%s", subject.Namespace, subject.Name)
				saResource := k8s.GenerateResourceForGrant(saName, k8s.ResourceTypeServiceAccount.Id)
				g := grant.NewGrant(
					resource,
					clusterScopedMember,
					saResource,
				)
				rv = append(rv, g)
			}
			if (subject.APIGroup == k8s.RBACAPIGroup || subject.APIGroup == k8s.RBACAPIGroupV1) &&
				!strings.Contains(subject.Name, "system:") {
				var matchingARNs []string
				switch subject.Kind {
				case k8s.SubjectKindGroup:
					matchingARNs, err = c.userMappingsProvider.LookupArnsByGroup(ctx, subject.Name)
					if err != nil {
						return nil, "", nil, fmt.Errorf("failed to lookup ARNs for group %s: %w", subject.Name, err)
					}
				case k8s.SubjectKindUser:
					matchingARNs, err = c.userMappingsProvider.LookupArnsByUsername(ctx, subject.Name)
					if err != nil {
						return nil, "", nil, fmt.Errorf("failed to lookup ARNs for user %s: %w", subject.Name, err)
					}
				}
				rv = append(rv, processGrants(matchingARNs, resource, entName)...)
			}
		}
	}

	return rv, "", nil, nil
}

// getNamespaces returns cached namespaces or fetches them if cache is expired or empty.
func (c *clusterRoleBuilder) cacheNamespaces(ctx context.Context) error {
	c.nsMutex.Lock()
	defer c.nsMutex.Unlock()

	now := time.Now()
	if c.cachedNamespaces != nil && now.Before(c.nsCacheExpiry) {
		// Cache is valid.
		return nil
	}
	var (
		names      []string
		continueAt string
	)
	for {
		opts := metav1.ListOptions{
			Continue: continueAt,
		}
		nsList, err := c.client.CoreV1().Namespaces().List(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to cache namespaces list: %w", err)
		}
		for _, ns := range nsList.Items {
			names = append(names, ns.Name)
		}
		if nsList.Continue == "" {
			break
		}
		continueAt = nsList.Continue
	}

	c.cachedNamespaces = names
	c.nsCacheExpiry = now.Add(cacheTTL)
	return nil
}

// newClusterRoleBuilder creates a new cluster role builder.
func NewClusterRoleBuilder(client kubernetes.Interface, bindingProvider k8s.ClusterRoleBindingProvider, provider *client.EKSClient) *clusterRoleBuilder {
	return &clusterRoleBuilder{
		client:               client,
		bindingProvider:      bindingProvider,
		userMappingsProvider: provider,
	}
}
