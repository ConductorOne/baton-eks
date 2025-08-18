package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

const cacheTTL = 5 * time.Minute

type EKSClient struct {
	kubernetes     *kubernetes.Clientset
	eksClient      *eks.Client
	iamClient      *iam.Client
	clusterName    string
	cacheUsersMap  map[string][]string
	cacheGroupsMap map[string][]string
	identityMutex  sync.Mutex
	idCacheExpiry  time.Time
}

func NewClient(cfg *rest.Config, awsCfg aws.Config, clusterName string) (*EKSClient, error) {
	// Create kubernetes client
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	// Create EKS client
	eksClient := eks.NewFromConfig(awsCfg)

	// Create IAM client
	iamClient := iam.NewFromConfig(awsCfg)

	return &EKSClient{
		kubernetes:     client,
		eksClient:      eksClient,
		iamClient:      iamClient,
		clusterName:    clusterName,
		cacheUsersMap:  make(map[string][]string),
		cacheGroupsMap: make(map[string][]string),
	}, nil
}

// Load or refresh the identity cache (user/group mappings from aws-auth and access entries).
func (c *EKSClient) LoadIdentityCacheMaps(ctx context.Context) error {
	l := ctxzap.Extract(ctx)
	c.identityMutex.Lock()
	defer c.identityMutex.Unlock()

	now := time.Now()
	if c.cacheUsersMap != nil && c.cacheGroupsMap != nil && now.Before(c.idCacheExpiry) {
		return nil
	}

	clear(c.cacheUsersMap)
	clear(c.cacheGroupsMap)

	// Initialize maps to store merged results
	userMap := make(map[string][]string)  // k8s username -> AWS user ARN list
	groupMap := make(map[string][]string) // k8s group -> AWS user ARN list

	awsAuthAccessible := true
	// Get aws-auth ConfigMap mappings
	awsAuthUserMap, awsAuthGroupMap, err := c.getAwsAuthMappings(ctx)
	if err != nil {
		// Log the error but continue with access entries
		// aws-auth might not exist or be accessible
		l.Debug("aws-auth ConfigMap not accessible, continuing with Access Entries", zap.Error(err))
		awsAuthAccessible = false
	} else {
		// Log aws-auth mappings found
		totalUsers := 0
		totalGroups := 0
		for _, arns := range awsAuthUserMap {
			totalUsers += len(arns)
		}
		for _, arns := range awsAuthGroupMap {
			totalGroups += len(arns)
		}
		l.Debug("Found aws-auth mappings",
			zap.Int("user_mappings", len(awsAuthUserMap)),
			zap.Int("total_user_arns", totalUsers),
			zap.Int("group_mappings", len(awsAuthGroupMap)),
			zap.Int("total_group_arns", totalGroups))
	}

	// Merge aws-auth mappings into the result maps
	for username, arns := range awsAuthUserMap {
		userMap[username] = append(userMap[username], arns...)
	}
	for group, arns := range awsAuthGroupMap {
		groupMap[group] = append(groupMap[group], arns...)
	}

	// Get Access Entries mappings
	accessEntriesUserMap, accessEntriesGroupMap, err := c.getAccessEntriesMappings(ctx)
	if err != nil {
		// Log the error but continue with aws-auth
		// Access entries might not exist or be accessible
		l.Debug("Access Entries not accessible, continuing with aws-auth", zap.Error(err))
		if !awsAuthAccessible {
			return fmt.Errorf("no users mapping found, failed to get aws-auth mappings and access entries: %w", err)
		}
	} else {
		// Log Access Entries mappings found
		totalUsers := 0
		totalGroups := 0
		for _, arns := range accessEntriesUserMap {
			totalUsers += len(arns)
		}
		for _, arns := range accessEntriesGroupMap {
			totalGroups += len(arns)
		}
		l.Debug("Found Access Entries mappings",
			zap.Int("user_mappings", len(accessEntriesUserMap)),
			zap.Int("total_user_arns", totalUsers),
			zap.Int("group_mappings", len(accessEntriesGroupMap)),
			zap.Int("total_group_arns", totalGroups))
	}

	// Merge Access Entries mappings into the result maps
	for username, arns := range accessEntriesUserMap {
		userMap[username] = append(userMap[username], arns...)
	}
	for group, arns := range accessEntriesGroupMap {
		groupMap[group] = append(groupMap[group], arns...)
	}

	// Log final merged results
	totalFinalUsers := 0
	totalFinalGroups := 0
	for _, arns := range userMap {
		totalFinalUsers += len(arns)
	}
	for _, arns := range groupMap {
		totalFinalGroups += len(arns)
	}
	l.Debug("Merged authentication mappings",
		zap.Int("total_user_mappings", len(userMap)),
		zap.Int("total_user_arns", totalFinalUsers),
		zap.Int("total_group_mappings", len(groupMap)),
		zap.Int("total_group_arns", totalFinalGroups))

	c.cacheUsersMap = userMap
	c.cacheGroupsMap = groupMap
	c.idCacheExpiry = now.Add(cacheTTL)
	return nil
}

// Lookup AWS ARNs by Kubernetes username.
func (c *EKSClient) LookupArnsByUsername(ctx context.Context, username string) ([]string, error) {
	if err := c.LoadIdentityCacheMaps(ctx); err != nil {
		return nil, err
	}
	return c.cacheUsersMap[username], nil
}

// Lookup AWS ARNs by Kubernetes group.
func (c *EKSClient) LookupArnsByGroup(ctx context.Context, group string) ([]string, error) {
	if err := c.LoadIdentityCacheMaps(ctx); err != nil {
		return nil, err
	}
	return c.cacheGroupsMap[group], nil
}

// Fetch and parse aws-auth ConfigMap.
func (c *EKSClient) getAwsAuthMappings(ctx context.Context) (map[string][]string, map[string][]string, error) {
	cm, err := c.kubernetes.CoreV1().ConfigMaps("kube-system").Get(ctx, "aws-auth", metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get aws-auth configmap: %w", err)
	}

	userMap := make(map[string][]string)  // k8s username -> AWS user ARN list
	groupMap := make(map[string][]string) // k8s group -> AWS user ARN list

	// Parse mapUsers
	if usersYaml, ok := cm.Data["mapUsers"]; ok && strings.TrimSpace(usersYaml) != "" {
		var users []mapUser
		if err := yaml.Unmarshal([]byte(usersYaml), &users); err == nil {
			for _, u := range users {
				if u.Username != "" {
					users := userMap[u.Username]
					userMap[u.Username] = append(users, u.UserARN)
				}
				if len(u.Groups) > 0 {
					// User can belong to multiple groups
					for _, group := range u.Groups {
						groups := groupMap[group]
						groupMap[group] = append(groups, u.UserARN)
					}
				}
			}
		}
	}
	// Parse mapRoles
	if rolesYaml, ok := cm.Data["mapRoles"]; ok && strings.TrimSpace(rolesYaml) != "" {
		var iamRoles []mapRole
		if err := yaml.Unmarshal([]byte(rolesYaml), &iamRoles); err == nil {
			for _, iamRole := range iamRoles {
				if iamRole.Username != "" {
					roles := userMap[iamRole.Username]
					userMap[iamRole.Username] = append(roles, iamRole.RoleARN)
				}
				if len(iamRole.Groups) > 0 {
					// User can belong to multiple groups
					for _, group := range iamRole.Groups {
						groups := groupMap[group]
						groupMap[group] = append(groups, iamRole.RoleARN)
					}
				}
			}
		}
	}
	return userMap, groupMap, nil
}

// ListIAMRoles lists IAM roles with pagination support
func (c *EKSClient) ListIAMRoles(ctx context.Context, nextToken *string) ([]*IAMRole, *string, error) {
	var roles []*IAMRole

	input := &iam.ListRolesInput{
		MaxItems: aws.Int32(100),
	}
	if nextToken != nil && *nextToken != "" {
		input.Marker = nextToken
	}
	page, err := c.iamClient.ListRoles(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list IAM roles: %w", err)
	}

	for _, role := range page.Roles {
		roles = append(roles, &IAMRole{
			RoleName:   *role.RoleName,
			RoleID:     *role.RoleId,
			ARN:        *role.Arn,
			CreateDate: role.CreateDate,
			Path:       role.Path,
		})
	}

	return roles, page.Marker, nil
}

// GetIAMRole gets details for a specific IAM role
func (c *EKSClient) GetIAMRole(ctx context.Context, roleName string) (*IAMRole, error) {
	result, err := c.iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get IAM role %s: %w", roleName, err)
	}

	role := result.Role
	return &IAMRole{
		RoleName:   *role.RoleName,
		RoleID:     *role.RoleId,
		ARN:        *role.Arn,
		CreateDate: role.CreateDate,
		Path:       role.Path,
	}, nil
}

// GetIAMRoleTrustPolicy gets the trust policy for a specific IAM role
func (c *EKSClient) GetIAMRoleTrustPolicy(ctx context.Context, roleName string) (string, error) {
	result, err := c.iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to get IAM role %s: %w", roleName, err)
	}

	if result.Role.AssumeRolePolicyDocument == nil {
		return "", fmt.Errorf("no trust policy found for role %s", roleName)
	}

	return *result.Role.AssumeRolePolicyDocument, nil
}

// TrustPolicy represents the structure of an IAM role trust policy
type TrustPolicy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

// Statement represents a statement in the trust policy
type Statement struct {
	Effect    string                 `json:"Effect"`
	Principal map[string]interface{} `json:"Principal"`
	Action    interface{}            `json:"Action"`
	Condition map[string]interface{} `json:"Condition,omitempty"`
}

// GetIAMRoleTrustPrincipals gets the principals that can assume a specific IAM role
func (c *EKSClient) GetIAMRoleTrustPrincipals(ctx context.Context, roleName string) ([]string, error) {
	trustPolicyJSON, err := c.GetIAMRoleTrustPolicy(ctx, roleName)
	if err != nil {
		return nil, err
	}

	// URL decode the trust policy document as AWS IAM API returns URL-encoded JSON
	decodedPolicy, err := url.QueryUnescape(trustPolicyJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to URL decode trust policy for role %s: %w", roleName, err)
	}

	var trustPolicy TrustPolicy
	if err := json.Unmarshal([]byte(decodedPolicy), &trustPolicy); err != nil {
		return nil, fmt.Errorf("failed to parse trust policy for role %s: %w", roleName, err)
	}

	var principals []string
	for _, statement := range trustPolicy.Statement {
		if statement.Effect == "Allow" {
			// Check if the action allows assuming the role
			actions := c.extractActions(statement.Action)
			for _, action := range actions {
				if action == "sts:AssumeRole" {
					// Extract principals from the statement
					statementPrincipals := c.extractPrincipals(statement.Principal)
					principals = append(principals, statementPrincipals...)
				}
			}
		}
	}

	return principals, nil
}

// extractActions extracts actions from the Action field which can be a string or array
func (c *EKSClient) extractActions(action interface{}) []string {
	var actions []string
	switch v := action.(type) {
	case string:
		actions = append(actions, v)
	case []interface{}:
		for _, a := range v {
			if actionStr, ok := a.(string); ok {
				actions = append(actions, actionStr)
			}
		}
	}
	return actions
}

// extractPrincipals extracts principal ARNs from the Principal field
func (c *EKSClient) extractPrincipals(principal map[string]interface{}) []string {
	var principals []string

	// Handle AWS principals
	if awsPrincipals, ok := principal["AWS"]; ok {
		principals = append(principals, c.extractPrincipalValues(awsPrincipals)...)
	}

	return principals
}

// extractPrincipalValues extracts values from principal fields which can be string or array
func (c *EKSClient) extractPrincipalValues(principal interface{}) []string {
	var values []string
	switch v := principal.(type) {
	case string:
		values = append(values, v)
	case []interface{}:
		for _, p := range v {
			if principalStr, ok := p.(string); ok {
				values = append(values, principalStr)
			}
		}
	}
	return values
}
