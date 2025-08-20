package connector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	awsSdk "github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/conductorone/baton-eks/pkg/client"
	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

type Connector struct {
	k8s                     *k8s.Kubernetes
	eksClient               *client.EKSClient
	_onceCallingConfig      map[string]*sync.Once
	_callingConfig          map[string]awsSdk.Config
	_callingConfigError     map[string]error
	globalRegion            string
	region                  string
	roleARN                 string
	externalID              string
	globalBindingExternalID string
	globalRoleARN           string
	globalSecretAccessKey   string
	globalAccessKeyID       string
	baseConfig              awsSdk.Config
	baseClient              *http.Client
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

type Config struct {
	GlobalBindingExternalID string
	GlobalRegion            string
	GlobalRoleARN           string
	GlobalSecretAccessKey   string
	GlobalAccessKeyID       string
	ExternalID              string
	RoleARN                 string
	ClusterName             string
	Region                  string
}

// New returns a new instance of the connector.
// EKS connector is a wrapper around the Kubernetes connector,
// handles EKS specific logic like authentication and user mappings.
func New(
	ctx context.Context,
	config Config,
) (*Connector, error) {
	l := ctxzap.Extract(ctx)
	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, err
	}

	opts := GetAwsConfigOptions(httpClient, config)
	baseConfig, err := awsConfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("eks connector: config load failure: %w", err)
	}

	newConnector := &Connector{
		baseConfig:              baseConfig.Copy(),
		baseClient:              httpClient,
		globalRegion:            config.GlobalRegion,
		region:                  config.Region,
		roleARN:                 config.RoleARN,
		externalID:              config.ExternalID,
		globalBindingExternalID: config.GlobalBindingExternalID,
		globalRoleARN:           config.GlobalRoleARN,
		globalAccessKeyID:       config.GlobalAccessKeyID,
		globalSecretAccessKey:   config.GlobalSecretAccessKey,
		_onceCallingConfig:      map[string]*sync.Once{},
		_callingConfig:          map[string]awsSdk.Config{},
		_callingConfigError:     map[string]error{},
	}

	connectorOpts := k8s.WithSyncResources([]string{
		k8s.ResourceTypeConfigMap.Id,
		k8s.ResourceTypeClusterRole.Id,
		k8s.ResourceTypeNamespace.Id,
		k8s.ResourceTypeRole.Id,
		k8s.ResourceTypeServiceAccount.Id,
	})

	iamClient, eksSDKClient, err := newConnector.SetupClients(ctx)
	if err != nil {
		l.Error("error creating EKS client", zap.Error(err))
		return nil, err
	}

	// Get EKS cluster configuration
	eksCfg, err := getEKSClusterCfg(ctx, eksSDKClient, config.Region, config.ClusterName)
	if err != nil {
		l.Error("failed to get EKS cluster config", zap.Error(err))
		return nil, err
	}

	// Get the AWS config with assumed role credentials for token generation
	callingConfig, err := newConnector.getCallingConfig(ctx, config.Region)
	if err != nil {
		l.Error("failed to get calling config", zap.Error(err))
		return nil, err
	}

	restConfig, err := newKubernetesConfig(ctx, eksCfg, callingConfig)
	if err != nil {
		l.Error("failed to create Kubernetes config", zap.Error(err))
		return nil, err
	}

	eksClient, err := client.NewEKSClient(restConfig, iamClient, eksSDKClient, config.ClusterName)
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

	newConnector.k8s = cb
	newConnector.eksClient = eksClient
	return newConnector, nil
}

func GetAwsConfigOptions(httpClient *http.Client, config Config) []func(*awsConfig.LoadOptions) error {
	opts := []func(*awsConfig.LoadOptions) error{
		awsConfig.WithHTTPClient(httpClient),
		awsConfig.WithRegion(config.GlobalRegion),
		awsConfig.WithDefaultsMode(awsSdk.DefaultsModeInRegion),
	}
	// Either we have an access key directly into our binding account, or we use
	// instance identity to swap into that role.
	if config.GlobalAccessKeyID != "" && config.GlobalSecretAccessKey != "" {
		opts = append(opts,
			awsConfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(
					config.GlobalAccessKeyID,
					config.GlobalSecretAccessKey,
					"",
				),
			),
		)
	}

	return opts
}

func (o *Connector) getCallingConfig(ctx context.Context, region string) (awsSdk.Config, error) {
	if _, ok := o._onceCallingConfig[region]; !ok {
		o._onceCallingConfig[region] = new(sync.Once)
	}
	o._onceCallingConfig[region].Do(func() {
		o._callingConfig[region], o._callingConfigError[region] = func() (awsSdk.Config, error) {
			if o.externalID == "" {
				return o.baseConfig, nil
			}
			l := ctxzap.Extract(ctx)
			// ok, if we are an instance, we do the assumeRole twice, first time from our Instance role, INTO the binding account
			// and from there, into the customer account.
			stsSvc := sts.NewFromConfig(o.baseConfig)
			bindingCreds := awsSdk.NewCredentialsCache(stscreds.NewAssumeRoleProvider(stsSvc, o.globalRoleARN, func(aro *stscreds.AssumeRoleOptions) {
				if o.globalBindingExternalID != "" {
					aro.ExternalID = awsSdk.String(o.globalBindingExternalID)
				}
			}))

			_, err := bindingCreds.Retrieve(ctx)
			if err != nil {
				l.Error("eks-connector: internal binding error",
					zap.Error(err),
					zap.String("binding_role_arn", o.globalRoleARN),
					zap.String("binding_external_id", o.globalBindingExternalID),
				)
				// we don't want to leak our assume role from our instance identity to a customer visible error
				return awsSdk.Config{}, fmt.Errorf("eks-connector: internal binding error")
			}

			// ok, now we have a working binding credentials.... lets go.
			stsConfig := o.baseConfig.Copy()
			stsConfig.Credentials = bindingCreds

			callingSTSService := sts.NewFromConfig(stsConfig)

			callingConfig := awsSdk.Config{
				HTTPClient:   o.baseClient,
				Region:       region,
				DefaultsMode: awsSdk.DefaultsModeInRegion,
				Credentials: awsSdk.NewCredentialsCache(stscreds.NewAssumeRoleProvider(callingSTSService, o.roleARN, func(aro *stscreds.AssumeRoleOptions) {
					aro.ExternalID = awsSdk.String(o.externalID)
				})),
			}

			// this is ok, since the cache will keep them.  we want to centralize error handling for this.
			_, err = callingConfig.Credentials.Retrieve(ctx)
			if err != nil {
				return awsSdk.Config{}, fmt.Errorf("eks-connector: unable to assume role into '%s/%s': %w", o.roleARN, o.externalID, err)
			}
			return callingConfig, nil
		}()
	})
	return o._callingConfig[region], o._callingConfigError[region]
}

func (c *Connector) SetupClients(ctx context.Context) (*iam.Client, *eks.Client, error) {
	globalCallingConfig, err := c.getCallingConfig(ctx, c.region)
	if err != nil {
		return nil, nil, err
	}

	iamClient := iam.NewFromConfig(globalCallingConfig)
	// Use the specific region where the EKS cluster exists, not the global region
	callingConfig, err := c.getCallingConfig(ctx, c.region)
	if err != nil {
		return nil, nil, err
	}
	eksClient := eks.NewFromConfig(callingConfig)

	return iamClient, eksClient, nil
}
