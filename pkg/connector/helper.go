package connector

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/conductorone/baton-eks/pkg/client"
	k8s "github.com/conductorone/baton-kubernetes/pkg/connector"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"
)

func processGrants(matchingARNs []string, resource *v2.Resource, entID string) []*v2.Grant {
	var rv []*v2.Grant
	if len(matchingARNs) > 0 {
		// Multiple users can be mapped to the same group.
		for _, principalARN := range matchingARNs {
			var grantOpts []grant.GrantOption
			resourceType := ResourceTypeIAMUser
			if strings.Contains(principalARN, ":role/") {
				resourceType = ResourceTypeIAMRole
				grantExpandable := &v2.GrantExpandable{
					EntitlementIds: []string{
						fmt.Sprintf("iam_role:%s:assumes", principalARN),
					},
				}
				grantOpts = append(grantOpts, grant.WithAnnotation(grantExpandable))
			} else {
				grantOpts = append(grantOpts, grant.WithAnnotation(&v2.ExternalResourceMatchID{
					Id: principalARN,
				}))
			}
			principalResource := k8s.GenerateResourceForGrant(principalARN, resourceType.Id)
			g := grant.NewGrant(
				resource,
				entID,
				principalResource,
				grantOpts...,
			)
			rv = append(rv, g)
		}
	}
	return rv
}

// getEKSClusterConfig retrieves the EKS cluster details.
func getEKSClusterCfg(ctx context.Context, eksClient *eks.Client, region string, clusterName string) (*client.EKSConfig, error) {
	l := ctxzap.Extract(ctx)

	// Describe the EKS cluster
	result, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		l.Error("failed to describe EKS cluster",
			zap.String("clusterName", clusterName),
			zap.Error(err),
		)
		return nil, err
	}
	l.Info("Cluster information", zap.Any("result", result))
	if result.Cluster == nil {
		return nil, fmt.Errorf("EKS cluster %s not found", clusterName)
	}
	caData, err := base64.StdEncoding.DecodeString(*result.Cluster.CertificateAuthority.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA data: %w", err)
	}

	parsedURL, err := url.Parse(*result.Cluster.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("error parsing cluster endpoint URL: %w", err)
	}
	serverName := parsedURL.Hostname()
	return &client.EKSConfig{
		ClusterServerName: serverName,
		ClusterName:       *result.Cluster.Name,
		Region:            region,
		Endpoint:          *result.Cluster.Endpoint,
		CAData:            caData,
	}, nil
}

// newKubernetesClient creates a new Kubernetes clientset for an EKS cluster.
func newKubernetesConfig(ctx context.Context, eksCfg *client.EKSConfig, awsConfig aws.Config) (*rest.Config, error) {
	l := ctxzap.Extract(ctx)
	err := validatePEMCertificate(eksCfg.CAData)
	if err != nil {
		l.Error("CAData is invalid", zap.Error(err))
		return nil, err
	}

	token, err := client.GenerateEKSTokenWithCredentials(ctx, eksCfg.ClusterName, eksCfg.Region, awsConfig)
	if err != nil {
		l.Error("Failed to generate EKS token", zap.Error(err))
		return nil, err
	}

	return &rest.Config{
		Host:        eksCfg.Endpoint,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:     eksCfg.CAData,
			ServerName: eksCfg.ClusterServerName,
		},
		// The custom RoundTripper will handle the BearerToken refresh logic
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			if rt == nil {
				rt = http.DefaultTransport
			}
			return &client.EksTokenRefreshRoundTripper{
				ClusterID: eksCfg.ClusterName,
				Region:    eksCfg.Region,
				Base:      rt,
				AwsConfig: awsConfig,
			}
		},
	}, nil
}

// Returns nil if valid, error if not.
func validatePEMCertificate(caData []byte) error {
	block, _ := pem.Decode(caData)
	if block == nil {
		return fmt.Errorf("CA data is not valid PEM")
	}
	_, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("CA data is not a valid certificate: %w", err)
	}
	return nil
}
