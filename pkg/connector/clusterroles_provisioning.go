package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	rbacv1 "k8s.io/api/rbac/v1"
)

// Grant implements ResourceProvisionerV2 interface to grant cluster role access.
func (c *clusterRoleBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	var (
		annotations annotations.Annotations
		err         error
		username    string
	)

	if principal.Id.ResourceType != ResourceTypeIAMUser.Id {
		return nil, fmt.Errorf("principal type is not supported")
	}

	// Extract entitlement ID to determine scope
	entitlementID := entitlement.Id
	if entitlementID == "" {
		return nil, fmt.Errorf("invalid entitlement ID")
	}

	if principal.Id.ResourceType == ResourceTypeIAMUser.Id {
		iamUserMap, err := c.eksService.GetMapUserFromAWSAuthConfigMap(ctx, principal.Id.Resource)
		if err != nil {
			return nil, fmt.Errorf("failed to get map user from aws-auth ConfigMap: %w", err)
		}
		if iamUserMap == nil {
			// There is no mapping for this user. We create a new mapping.
			err = c.eksService.AddIAMUserMapping(ctx, principal.Id.Resource)
			if err != nil {
				return nil, fmt.Errorf("failed to add IAM user mapping: %w", err)
			}
			username = principal.Id.Resource // For new users we use the ARN as the username
		} else {
			username = iamUserMap.Username
		}
	} else {
		return nil, fmt.Errorf("principal type %s is not supported", principal.Id.ResourceType)
	}
	// Determine if this is a cluster-scoped or namespace-scoped entitlement
	namespace, err := getNamespaceFromEntitlementID(entitlementID)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace from entitlement ID: %w", err)
	}

	// Create the appropriate binding based on scope
	if namespace == "" {
		// Cluster-scoped binding
		annotations, err = c.handleClusterRoleBinding(ctx, entitlement, username)
		if err != nil {
			return nil, fmt.Errorf("failed to handle cluster role binding: %w", err)
		}
	} else {
		// Namespace-scoped binding
		annotations, err = c.handleRoleBinding(ctx, entitlement, namespace, username)
		if err != nil {
			return nil, fmt.Errorf("failed to handle role binding: %w", err)
		}
	}

	return annotations, nil
}

func (c *clusterRoleBuilder) handleClusterRoleBinding(ctx context.Context, entitlement *v2.Entitlement, username string) (annotations.Annotations, error) {
	clusterRoleName := entitlement.Resource.Id.Resource
	bindingName := fmt.Sprintf("baton-%s-binding", clusterRoleName)
	_, matchingClusterBindings, err := c.bindingProvider.GetMatchingBindingsForClusterRole(ctx, clusterRoleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching bindings: %w", err)
	}

	if len(matchingClusterBindings) > 0 {
		var bindingToUpdate *rbacv1.ClusterRoleBinding
		for _, binding := range matchingClusterBindings {
			if binding.Name == bindingName {
				bindingToUpdate = &binding
			}
			if userAlreadyHasAccess(binding.Subjects, username) {
				return annotations.New(&v2.GrantAlreadyExists{}), nil
			}
		}
		if bindingToUpdate != nil {
			bindingToUpdate.Subjects = append(bindingToUpdate.Subjects, rbacv1.Subject{
				Kind:     "User",
				Name:     username,
				APIGroup: rbacv1.GroupName,
			})
			err = c.eksService.UpdateClusterRoleBinding(ctx, bindingToUpdate)
			if err != nil {
				return nil, fmt.Errorf("failed to update cluster role binding: %w", err)
			}
			return nil, nil
		}
	}
	// Binding doesn't exist, create a new binding.
	err = c.createClusterRoleBinding(ctx, clusterRoleName, username)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster role binding: %w", err)
	}

	return nil, nil
}

func (c *clusterRoleBuilder) createClusterRoleBinding(ctx context.Context, clusterRoleName string, subjectName string) error {
	bindingName := fmt.Sprintf("baton-%s-binding", clusterRoleName)
	subjects := []rbacv1.Subject{
		{
			Kind:     "User",
			Name:     subjectName,
			APIGroup: rbacv1.GroupName,
		},
	}
	err := c.eksService.CreateClusterRoleBinding(ctx, bindingName, clusterRoleName, subjects)
	if err != nil {
		return fmt.Errorf("failed to create cluster role binding: %w", err)
	}
	return nil
}

func (c *clusterRoleBuilder) handleRoleBinding(ctx context.Context, entitlement *v2.Entitlement, namespace string, username string) (annotations.Annotations, error) {
	clusterRoleName := entitlement.Resource.Id.Resource
	bindingName := fmt.Sprintf("baton-%s-%s-binding", clusterRoleName, namespace)
	matchingRoleBindings, _, err := c.bindingProvider.GetMatchingBindingsForClusterRole(ctx, clusterRoleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching bindings: %w", err)
	}

	if len(matchingRoleBindings) > 0 {
		var bindingToUpdate *rbacv1.RoleBinding
		for _, binding := range matchingRoleBindings {
			if binding.Name == bindingName {
				bindingToUpdate = &binding
			}
			if userAlreadyHasAccess(binding.Subjects, username) {
				return annotations.New(&v2.GrantAlreadyExists{}), nil
			}
		}
		if bindingToUpdate != nil {
			bindingToUpdate.Subjects = append(bindingToUpdate.Subjects, rbacv1.Subject{
				Kind:     "User",
				Name:     username,
				APIGroup: rbacv1.GroupName,
			})
			err = c.eksService.UpdateRoleBinding(ctx, bindingToUpdate)
			if err != nil {
				return nil, fmt.Errorf("failed to update cluster role binding: %w", err)
			}
			return nil, nil
		}
	}
	// Binding doesn't exist, create a new binding.
	err = c.createRoleBinding(ctx, namespace, clusterRoleName, username)
	if err != nil {
		return nil, fmt.Errorf("failed to create role binding: %w", err)
	}

	return nil, nil
}

func (c *clusterRoleBuilder) createRoleBinding(ctx context.Context, namespace string, clusterRoleName string, subjectName string) error {
	bindingName := fmt.Sprintf("baton-%s-%s-binding", clusterRoleName, namespace)
	subjects := []rbacv1.Subject{
		{
			Kind:     "User",
			Name:     subjectName,
			APIGroup: rbacv1.GroupName,
		},
	}
	roleRef := rbacv1.RoleRef{
		Kind:     "ClusterRole",
		Name:     clusterRoleName,
		APIGroup: rbacv1.GroupName,
	}
	err := c.eksService.CreateRoleBinding(ctx, namespace, bindingName, roleRef, subjects)
	if err != nil {
		return fmt.Errorf("failed to create role binding: %w", err)
	}
	return nil
}

// Revoke implements ResourceProvisionerV2 interface to revoke cluster role access.
func (c *clusterRoleBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	var annos annotations.Annotations
	l := ctxzap.Extract(ctx)

	// Extract cluster role name from grant
	if grant.Entitlement == nil || grant.Entitlement.Resource == nil || grant.Entitlement.Resource.Id == nil {
		return nil, fmt.Errorf("invalid grant: missing entitlement resource")
	}
	clusterRoleName := grant.Entitlement.Resource.Id.Resource

	// Extract entitlement ID to determine scope
	entitlementID := grant.Entitlement.Id
	if entitlementID == "" {
		return nil, fmt.Errorf("invalid entitlement ID")
	}

	// Determine if this is a cluster-scoped or namespace-scoped entitlement
	namespace, err := getNamespaceFromEntitlementID(entitlementID)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace from entitlement ID: %w", err)
	}

	// Extract principal information
	principal := grant.Principal
	if principal == nil || principal.Id == nil {
		return nil, fmt.Errorf("invalid principal")
	}

	iamUserMap, err := c.eksService.GetMapUserFromAWSAuthConfigMap(ctx, principal.Id.Resource)
	if err != nil {
		return nil, fmt.Errorf("failed to get map user from aws-auth ConfigMap: %w", err)
	}

	if iamUserMap == nil {
		l.Debug("no mapping found in aws-auth ConfigMap for the principal, returning GrantAlreadyRevoked",
			zap.String("principal", principal.Id.Resource),
		)
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	// Revoke the appropriate binding based on scope
	if namespace == "" {
		// Cluster-scoped binding
		annos, err = c.revokeClusterRoleBinding(ctx, clusterRoleName, iamUserMap.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to revoke cluster role binding: %w", err)
		}
	} else {
		// Namespace-scoped binding
		annos, err = c.revokeRoleBinding(ctx, clusterRoleName, namespace, iamUserMap.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to revoke role binding: %w", err)
		}
	}

	l.Info("successfully revoked cluster role access",
		zap.String("cluster_role", clusterRoleName),
		zap.String("principal", principal.Id.Resource),
		zap.String("namespace", namespace),
		zap.String("entitlement", entitlementID))

	return annos, nil
}

// revokeClusterRoleBinding removes a subject from a ClusterRoleBinding.
func (c *clusterRoleBuilder) revokeClusterRoleBinding(ctx context.Context, clusterRoleName string, username string) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	_, matchingClusterBindings, err := c.bindingProvider.GetMatchingBindingsForClusterRole(ctx, clusterRoleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching bindings: %w", err)
	}
	bindingToUpdate := getExistingClusterRoleBindingForUser(matchingClusterBindings, username)
	if bindingToUpdate == nil {
		l.Debug("no matching cluster role binding found, returning GrantAlreadyRevoked",
			zap.String("cluster_role", clusterRoleName),
			zap.String("principal", username),
		)
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}
	newSubjects := filterSubjects(bindingToUpdate.Subjects, username)
	err = handleBindingUpdateOrDelete(ctx, c.eksService, bindingToUpdate, newSubjects, "")
	if err != nil {
		return nil, fmt.Errorf("failed to handle cluster role binding update/delete: %w", err)
	}

	return nil, nil
}

// revokeRoleBinding removes a subject from a RoleBinding.
func (c *clusterRoleBuilder) revokeRoleBinding(ctx context.Context, clusterRoleName string, namespace string, username string) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	matchingRoleBindings, _, err := c.bindingProvider.GetMatchingBindingsForClusterRole(ctx, clusterRoleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching bindings: %w", err)
	}
	bindingToUpdate := getExistingBindingForUser(matchingRoleBindings, username)
	if bindingToUpdate == nil {
		l.Debug("no matching cluster role binding found, returning GrantAlreadyRevoked",
			zap.String("cluster_role", clusterRoleName),
			zap.String("principal", username),
		)
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}
	newSubjects := filterSubjects(bindingToUpdate.Subjects, username)
	err = handleBindingUpdateOrDelete(ctx, c.eksService, bindingToUpdate, newSubjects, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to handle role binding update/delete for cluster role %s in namespace %s: %w", clusterRoleName, namespace, err)
	}

	return nil, nil
}
