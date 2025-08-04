package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

// Fetch and parse access entries from EKS API.
func (c *EKSClient) getAccessEntriesMappings(ctx context.Context) (map[string][]string, map[string][]string, error) {
	l := ctxzap.Extract(ctx)
	userMap := make(map[string][]string)  // k8s username -> AWS user ARN list
	groupMap := make(map[string][]string) // k8s group -> AWS user ARN list

	// List all access entries for the cluster
	paginator := eks.NewListAccessEntriesPaginator(c.eksClient, &eks.ListAccessEntriesInput{
		ClusterName: aws.String(c.clusterName),
	})

	var accessEntries []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list access entries: %w", err)
		}
		accessEntries = append(accessEntries, page.AccessEntries...)
	}

	// Process each access entry.
	// The accessEntryArn is the ARN of the IAM user or role in the access entry, not the access entry ARN.
	for _, accessEntryArn := range accessEntries {
		if !isUserOrRoleArn(accessEntryArn) {
			l.Debug("Skipping non-user/role ARN",
				zap.String("principal_arn", accessEntryArn))
			continue
		}

		// Get detailed information about the access entry
		describeResult, err := c.eksClient.DescribeAccessEntry(ctx, &eks.DescribeAccessEntryInput{
			ClusterName:  aws.String(c.clusterName),
			PrincipalArn: aws.String(accessEntryArn),
		})
		if err != nil {
			// Skip this access entry if we can't describe it
			continue
		}

		accessEntry := describeResult.AccessEntry
		if accessEntry == nil {
			continue
		}

		// // Only process STANDARD type access entries (not node types)
		if accessEntry.Type == nil || *accessEntry.Type != "STANDARD" {
			continue
		}

		// For Access Entries, the principal ARN is the primary identity
		principalArn := *accessEntry.PrincipalArn

		// Map username if present, otherwise use principal ARN as username
		if accessEntry.Username != nil && *accessEntry.Username != "" {
			// Use the specified username
			users := userMap[*accessEntry.Username]
			userMap[*accessEntry.Username] = append(users, principalArn)
		} else {
			// Use the principal ARN as the username (Access Entries style)
			users := userMap[principalArn]
			userMap[principalArn] = append(users, principalArn)
		}

		// Map groups if present (these come from Kubernetes RBAC if specified)
		if accessEntry.KubernetesGroups != nil {
			for _, group := range accessEntry.KubernetesGroups {
				groups := groupMap[group]
				groupMap[group] = append(groups, principalArn)
			}
		}
	}

	return userMap, groupMap, nil
}

// GetAccessEntriesWithPolicy retrieves principal ARNs that have a specific policy assigned.
func (c *EKSClient) GetAccessEntriesWithPolicy(ctx context.Context, policyARN string, nextToken *string) ([]string, *string, error) {
	var principalARNs []string

	input := &eks.ListAccessEntriesInput{
		ClusterName:         aws.String(c.clusterName),
		AssociatedPolicyArn: aws.String(policyARN),
		MaxResults:          aws.Int32(100),
	}

	// Add nextToken if provided
	if nextToken != nil && *nextToken != "" {
		input.NextToken = nextToken
	}

	page, err := c.eksClient.ListAccessEntries(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list access entries: %w", err)
	}

	principalARNs = append(principalARNs, page.AccessEntries...)

	return principalARNs, page.NextToken, nil
}

// isUserOrRoleArn checks if an ARN is for an IAM user or role.
func isUserOrRoleArn(arn string) bool {
	// AWS ARN format: arn:aws:service:region:account:resource-type/resource-name
	// For IAM users: arn:aws:iam::account:user/username
	// For IAM roles: arn:aws:iam::account:role/rolename

	// Check if it's an IAM ARN
	if !strings.HasPrefix(arn, "arn:aws:iam::") {
		return false
	}

	// Check if it's a user or role
	return strings.Contains(arn, ":user/") || strings.Contains(arn, ":role/")
}
