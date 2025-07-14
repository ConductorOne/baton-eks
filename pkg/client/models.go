package client

// Structs for parsing mapUsers/mapRoles entries.
type mapUser struct {
	UserARN  string   `yaml:"userarn,omitempty"`
	Username string   `yaml:"username,omitempty"`
	Groups   []string `yaml:"groups,omitempty"`
}

// EKSConfig provides EKS cluster configuration.
type EKSConfig struct {
	ClusterServerName string
	ClusterName       string
	Region            string
	Endpoint          string
	CAData            []byte
}
