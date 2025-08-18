package connector

import (
	"context"
	"io"

	"github.com/conductorone/baton-eks/pkg/client"
	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

type Connector struct {
	k8s       *k8s.Kubernetes
	eksClient *client.EKSClient
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	syncers := d.k8s.ResourceSyncers(ctx)
	syncers = append(syncers,
		NewAccessPolicyBuilder(d.eksClient),
		NewIAMRoleBuilder(d.eksClient),
	)
	return syncers
}

// Asset takes an input AssetRef and attempts to fetch it using the connector's authenticated http client
// It streams a response, always starting with a metadata object, following by chunked payloads for the asset.
func (d *Connector) Asset(ctx context.Context, asset *v2.AssetRef) (string, io.ReadCloser, error) {
	return "", nil, nil
}

// Metadata returns metadata about the connector.
func (d *Connector) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Amazon EKS Connector",
		Description: "Connector for Amazon EKS. This connector syncs Kubernetes resources from EKS clusters.",
	}, nil
}

// Validate is called to ensure that the connector is properly configured. It should exercise any API credentials
// to be sure that they are valid.
func (d *Connector) Validate(ctx context.Context) (annotations.Annotations, error) {
	return nil, nil
}

// New returns a new instance of the connector.
// EKS connector is a wrapper around the Kubernetes connector,
// handles EKS specific logic like authentication and user mappings.
func New(
	ctx context.Context,
	accessKey string,
	secretKey string,
	region string,
	assumeRole string,
	clusterName string,
) (*Connector, error) {
	l := ctxzap.Extract(ctx)
	// Get AWS config
	awsCfg, err := getAWSConfig(ctx, accessKey, secretKey, region, assumeRole)
	if err != nil {
		return nil, err
	}

	// Get EKS cluster configuration
	eksCfg, err := getEKSClusterConfig(ctx, awsCfg, clusterName, region)
	if err != nil {
		l.Error("failed to get EKS cluster config", zap.Error(err))
		return nil, err
	}

	restConfig, err := newKubernetesConfig(ctx, eksCfg, assumeRole)
	if err != nil {
		l.Error("failed to create Kubernetes config", zap.Error(err))
		return nil, err
	}

	connectorOpts := k8s.WithSyncResources([]string{
		k8s.ResourceTypeConfigMap.Id,
		k8s.ResourceTypeClusterRole.Id,
		k8s.ResourceTypeNamespace.Id,
		k8s.ResourceTypeRole.Id,
		k8s.ResourceTypeServiceAccount.Id,
	})

	eksClient, err := client.NewClient(restConfig, awsCfg, clusterName)
	if err != nil {
		l.Error("error creating EKS client", zap.Error(err))
		return nil, err
	}

	syncersMap := make(map[string]k8s.ResourceSyncerBuilder)
	syncersMap[k8s.ResourceTypeClusterRole.Id] = func(client *kubernetes.Interface, k *k8s.Kubernetes) connectorbuilder.ResourceSyncer {
		return NewClusterRoleBuilder(*client, k, eksClient)
	}
	syncersMap[k8s.ResourceTypeRole.Id] = func(client *kubernetes.Interface, k *k8s.Kubernetes) connectorbuilder.ResourceSyncer {
		return NewRoleBuilder(*client, k, eksClient)
	}

	customSyncerOpt := k8s.WithCustomSyncers(syncersMap)

	cb, err := k8s.New(ctx, restConfig, connectorOpts, customSyncerOpt)
	if err != nil {
		l.Error("error creating k8s connector", zap.Error(err))
		return nil, err
	}
	return &Connector{
		k8s:       cb,
		eksClient: eksClient,
	}, nil
}
