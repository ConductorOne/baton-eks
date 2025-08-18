package client

import "time"

// Structs for parsing mapUsers/mapRoles entries.
type mapUser struct {
	UserARN  string   `yaml:"userarn,omitempty"`
	Username string   `yaml:"username,omitempty"`
	Groups   []string `yaml:"groups,omitempty"`
}

type mapRole struct {
	RoleARN  string   `yaml:"rolearn,omitempty"`
	Username string   `yaml:"username,omitempty"`
	Groups   []string `yaml:"groups,omitempty"`
}

// AccessEntry represents an EKS access entry.
type AccessEntry struct {
	AccessEntryArn   string
	ClusterName      string
	CreatedAt        string
	KubernetesGroups []string
	ModifiedAt       string
	PrincipalArn     string
	Tags             map[string]string
	Type             string
	Username         string
}

// AccessPolicy represents an EKS access policy.
type AccessPolicy struct {
	PolicyARN   string
	AccessScope AccessScope
	DisplayName string
	Description string
}

// AccessScope represents the scope of an access policy.
type AccessScope struct {
	Type string // "cluster", "namespace", etc.
}

// AccessEntryWithPolicies represents an access entry with its associated policies.
type AccessEntryWithPolicies struct {
	AccessEntry
	AccessPolicies []AccessPolicy
}

// EKSConfig provides EKS cluster configuration.
type EKSConfig struct {
	ClusterServerName string
	ClusterName       string
	Region            string
	Endpoint          string
	CAData            []byte
}

// IAMRole represents an AWS IAM role
type IAMRole struct {
	RoleName   string
	RoleID     string
	ARN        string
	CreateDate *time.Time
	Path       *string
}
