package client

import (
	"context"

	eksTypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AccessPolicyClient defines the interface for EKS client methods needed by the access policy builder.
type AccessPolicyClient interface {
	ListNamespaces(ctx context.Context, opts metav1.ListOptions) (*corev1.NamespaceList, error)
	GetAccessEntriesWithPolicy(ctx context.Context, policyARN string, nextToken *string) ([]string, *string, error)
	GetAssociatedAccessPolicies(ctx context.Context, principalARN string) ([]eksTypes.AssociatedAccessPolicy, error)
	CreateAccessEntry(ctx context.Context, principalARN string) (*eksTypes.AccessEntry, error)
	AssociateAccessPolicy(ctx context.Context, principalARN string, policyARN string, accessScope *eksTypes.AccessScope) error
	DisassociateAccessPolicy(ctx context.Context, principalARN string, policyARN string) error
}
