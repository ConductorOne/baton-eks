package client

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
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

	return &EKSClient{
		kubernetes:     client,
		eksClient:      eksClient,
		clusterName:    clusterName,
		cacheUsersMap:  make(map[string][]string),
		cacheGroupsMap: make(map[string][]string),
	}, nil
}

// Load or refresh the identity cache (user/group mappings from aws-auth and access entries).
func (c *EKSClient) LoadIdentityCacheMaps(ctx context.Context) error {
	// TODO: Currently we do not support the mapRoles section of aws-auth.
	// This is because  kubernetes groups are mapped to AWS IAM roles, but that wont show on c1
	// and the AWS connector does not handle the entitlement for users that can assume roles,
	// therefore we cannot expand the granted permissions from a IAM role to a user.
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
	return userMap, groupMap, nil
}
