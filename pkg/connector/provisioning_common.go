package connector

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/conductorone/baton-eks/pkg/client"
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
