package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
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

	username, err = getOrCreateUsername(ctx, c.eksService, principal)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create username: %w", err)
	}

	// Get matching role bindings for role from the binding provider.
	matchingBindings, err := c.bindingProvider.GetMatchingRoleBindings(ctx, namespace, roleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching role bindings: %w", err)
	}

	annotations, err = handleRoleBinding(ctx, c.eksService, namespace, username, "Role", matchingBindings, roleName)
	if err != nil {
		return nil, fmt.Errorf("failed to handle role binding: %w", err)
	}

	return annotations, nil
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

	l.Info("successfully revoked role access",
		zap.String("role", roleName),
		zap.String("principal", principal.Id.Resource),
		zap.String("namespace", namespace),
		zap.String("entitlement", grant.Entitlement.Id))

	return annos, nil
}

// revokeRoleBinding removes a subject from a RoleBinding.
func (c *roleBuilder) revokeRoleBinding(ctx context.Context, roleName string, namespace string, username string) (annotations.Annotations, error) {
	matchingBindings, err := c.bindingProvider.GetMatchingRoleBindings(ctx, namespace, roleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching role bindings: %w", err)
	}
	return handleRevokeRoleBinding(ctx, c.eksService, namespace, username, matchingBindings, roleName)
}
