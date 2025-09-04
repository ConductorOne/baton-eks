package connector

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/conductorone/baton-eks/pkg/client"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

const subjectKindUser = "User"

// filterSubjects removes a user from a list of subjects.
func filterSubjects(subjects []rbacv1.Subject, username string) []rbacv1.Subject {
	var newSubjects []rbacv1.Subject
	for _, existingSubject := range subjects {
		// Check if this subject matches the one we want to remove
		if existingSubject.Name == username && existingSubject.Kind == subjectKindUser {
			continue // Skip this subject (remove it)
		}
		newSubjects = append(newSubjects, existingSubject)
	}
	return newSubjects
}

// getExistingBindingForUser finds a binding that contains the specified user.
func getExistingBindingForUser(matchingRoleBindings []rbacv1.RoleBinding, username string) *rbacv1.RoleBinding {
	for _, binding := range matchingRoleBindings {
		if len(binding.Subjects) > 0 {
			for _, subject := range binding.Subjects {
				if subject.Name == username && subject.Kind == subjectKindUser {
					return &binding
				}
			}
		}
	}
	return nil
}

// getExistingClusterRoleBindingForUser finds a cluster role binding that contains the specified user.
func getExistingClusterRoleBindingForUser(matchingClusterRoleBindings []rbacv1.ClusterRoleBinding, username string) *rbacv1.ClusterRoleBinding {
	for _, binding := range matchingClusterRoleBindings {
		if len(binding.Subjects) > 0 {
			for _, subject := range binding.Subjects {
				if subject.Name == username && subject.Kind == subjectKindUser {
					return &binding
				}
			}
		}
	}
	return nil
}

// handleBindingUpdateOrDelete handles the common logic for updating or deleting a binding based on remaining subjects.
func handleBindingUpdateOrDelete(ctx context.Context, eksService *client.EKSClient, binding interface{}, newSubjects []rbacv1.Subject, namespace string) error {
	if len(newSubjects) == 0 {
		// No subjects left, delete the binding
		switch b := binding.(type) {
		case *rbacv1.RoleBinding:
			return eksService.DeleteRoleBinding(ctx, namespace, b.Name)
		case *rbacv1.ClusterRoleBinding:
			return eksService.DeleteClusterRoleBinding(ctx, b.Name)
		}
	} else {
		// Update binding with remaining subjects
		switch b := binding.(type) {
		case *rbacv1.RoleBinding:
			b.Subjects = newSubjects
			return eksService.UpdateRoleBinding(ctx, b)
		case *rbacv1.ClusterRoleBinding:
			b.Subjects = newSubjects
			return eksService.UpdateClusterRoleBinding(ctx, b)
		}
	}
	return nil
}

// userAlreadyHasAccess checks if a user already has access through a binding's subjects.
func userAlreadyHasAccess(subjects []rbacv1.Subject, username string) bool {
	if len(subjects) == 0 {
		return false
	}
	for _, subject := range subjects {
		if subject.Name == username && subject.Kind == subjectKindUser {
			return true
		}
	}
	return false
}

func handleRoleBinding(
	ctx context.Context,
	eksService *client.EKSClient,
	namespace string,
	username string,
	roleKind string,
	roleBindings []rbacv1.RoleBinding,
	roleName string,
) (annotations.Annotations, error) {
	var err error
	bindingName := fmt.Sprintf("baton-%s-%s-binding", roleName, namespace)
	if len(roleBindings) > 0 {
		var bindingToUpdate *rbacv1.RoleBinding
		for _, binding := range roleBindings {
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
			err = eksService.UpdateRoleBinding(ctx, bindingToUpdate)
			if err != nil {
				return nil, fmt.Errorf("failed to update role binding: %w", err)
			}
			return nil, nil
		}
	}
	// Binding doesn't exist, create a new binding.
	err = createRoleBinding(ctx, eksService, namespace, roleName, username, roleKind)
	if err != nil {
		return nil, fmt.Errorf("failed to create role binding: %w", err)
	}

	return nil, nil
}

func createRoleBinding(
	ctx context.Context,
	eksService *client.EKSClient,
	namespace string,
	roleName string,
	subjectName string,
	roleKind string,
) error {
	bindingName := fmt.Sprintf("baton-%s-%s-binding", roleName, namespace)
	subjects := []rbacv1.Subject{
		{
			Kind:     "User",
			Name:     subjectName,
			APIGroup: rbacv1.GroupName,
		},
	}
	roleRef := rbacv1.RoleRef{
		Kind:     roleKind,
		Name:     roleName,
		APIGroup: rbacv1.GroupName,
	}
	err := eksService.CreateRoleBinding(ctx, namespace, bindingName, roleRef, subjects)
	if err != nil {
		return fmt.Errorf("failed to create role binding: %w", err)
	}
	return nil
}

func handleRevokeRoleBinding(
	ctx context.Context,
	eksService *client.EKSClient,
	namespace string,
	username string,
	roleBindings []rbacv1.RoleBinding,
	roleName string,
) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)
	bindingToUpdate := getExistingBindingForUser(roleBindings, username)
	if bindingToUpdate == nil {
		l.Debug("no matching role binding found, returning GrantAlreadyRevoked",
			zap.String("role", roleName),
			zap.String("principal", username),
		)
		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}
	newSubjects := filterSubjects(bindingToUpdate.Subjects, username)
	err := handleBindingUpdateOrDelete(ctx, eksService, bindingToUpdate, newSubjects, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to handle binding update/delete for role %s in namespace %s: %w", roleName, namespace, err)
	}
	return nil, nil
}

func getOrCreateUsername(ctx context.Context, eksService *client.EKSClient, principal *v2.Resource) (string, error) {
	iamUserMap, err := eksService.GetMapUserFromAWSAuthConfigMap(ctx, principal.Id.Resource)
	if err != nil {
		return "", fmt.Errorf("failed to get map user from aws-auth ConfigMap: %w", err)
	}
	if iamUserMap == nil {
		err = eksService.AddIAMUserMapping(ctx, principal.Id.Resource)
		if err != nil {
			return "", fmt.Errorf("failed to add IAM user mapping: %w", err)
		}
		return principal.Id.Resource, nil
	}
	return iamUserMap.Username, nil
}
