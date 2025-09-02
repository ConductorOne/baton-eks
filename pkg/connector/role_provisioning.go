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

func (c *roleBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) (annotations.Annotations, error) {
	var (
		annotations annotations.Annotations
		err         error
		username    string
	)

	if principal.Id.ResourceType != ResourceTypeIAMUser.Id {
		return nil, fmt.Errorf("principal type is not supported")
	}

	namespace, roleName, err := parseRoleResourceID(entitlement.Resource.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace from entitlement ID: %w", err)
	}
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
	annotations, err = c.handleRoleBinding(ctx, namespace, username, roleName)
	if err != nil {
		return nil, fmt.Errorf("failed to handle role binding: %w", err)
	}

	return annotations, nil
}

func (c *roleBuilder) handleRoleBinding(ctx context.Context,
	namespace string,
	username string,
	roleName string,
) (annotations.Annotations, error) {
	// Generate binding name
	bindingName := fmt.Sprintf("baton-%s-%s-binding", roleName, namespace)

	// Get matching role bindings for role from the binding provider.
	matchingBindings, err := c.bindingProvider.GetMatchingRoleBindings(ctx, namespace, roleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching role bindings: %w", err)
	}

	if len(matchingBindings) > 0 {
		var bindingToUpdate *rbacv1.RoleBinding
		for _, binding := range matchingBindings {
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
	err = c.createRoleBinding(ctx, namespace, roleName, username)
	if err != nil {
		return nil, fmt.Errorf("failed to create role binding: %w", err)
	}

	return nil, nil
}

func (c *roleBuilder) createRoleBinding(ctx context.Context, namespace string, roleName string, subjectName string) error {
	bindingName := fmt.Sprintf("baton-%s-%s-binding", roleName, namespace)
	subjects := []rbacv1.Subject{
		{
			Kind:     "User",
			Name:     subjectName,
			APIGroup: rbacv1.GroupName,
		},
	}
	roleRef := rbacv1.RoleRef{
		Kind:     "Role",
		Name:     roleName,
		APIGroup: rbacv1.GroupName,
	}
	err := c.eksService.CreateRoleBinding(ctx, namespace, bindingName, roleRef, subjects)
	if err != nil {
		return fmt.Errorf("failed to create role binding: %w", err)
	}
	return nil
}

// Revoke implements ResourceProvisionerV2 interface to revoke role access for IAM user.
func (c *roleBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	var annos annotations.Annotations
	l := ctxzap.Extract(ctx)

	if grant.Entitlement == nil || grant.Entitlement.Resource == nil || grant.Entitlement.Resource.Id == nil {
		return nil, fmt.Errorf("invalid grant: missing entitlement resource")
	}
	namespace, roleName, err := parseRoleResourceID(grant.Entitlement.Resource.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace from entitlement ID: %w", err)
	}

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

	annos, err = c.revokeRoleBinding(ctx, roleName, namespace, iamUserMap.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to revoke role binding: %w", err)
	}

	l.Info("successfully revoked cluster role access",
		zap.String("role", roleName),
		zap.String("principal", principal.Id.Resource),
		zap.String("namespace", namespace),
		zap.String("entitlement", grant.Entitlement.Id))

	return annos, nil
}

// revokeRoleBinding removes a subject from a RoleBinding.
func (c *roleBuilder) revokeRoleBinding(ctx context.Context, roleName string, namespace string, username string) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	matchingBindings, err := c.bindingProvider.GetMatchingRoleBindings(ctx, namespace, roleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching role bindings: %w", err)
	}
	bindingToUpdate := getExistingBindingForUser(matchingBindings, username)
	if bindingToUpdate == nil {
		l.Debug("no matching role binding found, returning GrantAlreadyRevoked",
			zap.String("role", roleName),
			zap.String("principal", username),
		)
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}
	newSubjects := filterSubjects(bindingToUpdate.Subjects, username)
	err = handleBindingUpdateOrDelete(ctx, c.eksService, bindingToUpdate, newSubjects, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to handle binding update/delete for role %s in namespace %s: %w", roleName, namespace, err)
	}

	return nil, nil
}
