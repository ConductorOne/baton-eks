package client

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

const cacheTTL = 5 * time.Minute

type EKSClient struct {
	kubernetes     *kubernetes.Clientset
	cacheUsersMap  map[string][]string
	cacheGroupsMap map[string][]string
	identityMutex  sync.Mutex
	idCacheExpiry  time.Time
}

func NewClient(cfg *rest.Config) (*EKSClient, error) {
	// Create kubernetes client
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	return &EKSClient{
		kubernetes: client,
	}, nil
}

// Load or refresh the identity cache (user/group mappings from aws-auth).
func (c *EKSClient) LoadIdentityCacheMaps(ctx context.Context) error {
	// TODO: Currently we do not support the mapRoles section of aws-auth.
	// This is because  kubernetes groups are mapped to AWS IAM roles, but that wont show on c1
	// and the AWS connector does not handle the entitlement for users that can assume roles,
	// therefore we cannot expand the granted permissions from a IAM role to a user.
	c.identityMutex.Lock()
	defer c.identityMutex.Unlock()

	now := time.Now()
	if c.cacheUsersMap != nil && c.cacheGroupsMap != nil && now.Before(c.idCacheExpiry) {
		return nil
	}

	// Fetch and parse aws-auth ConfigMap
	userMap, groupMap, err := c.getAwsAuthMappings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get aws-auth mappings: %w", err)
	}

	c.cacheUsersMap = *userMap
	c.cacheGroupsMap = *groupMap
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

// Expose the full user/group maps if needed.
func (c *EKSClient) GetUserMap(ctx context.Context) (map[string][]string, error) {
	if err := c.LoadIdentityCacheMaps(ctx); err != nil {
		return nil, err
	}
	return c.cacheUsersMap, nil
}

func (c *EKSClient) GetGroupMap(ctx context.Context) (map[string][]string, error) {
	if err := c.LoadIdentityCacheMaps(ctx); err != nil {
		return nil, err
	}
	return c.cacheGroupsMap, nil
}

// Fetch and parse aws-auth ConfigMap.
func (c *EKSClient) getAwsAuthMappings(ctx context.Context) (*map[string][]string, *map[string][]string, error) {
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
	return &userMap, &groupMap, nil
}
