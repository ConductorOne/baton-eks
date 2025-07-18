//go:build !generate

package main

import (
	"context"
	"fmt"
	"os"

	eksCon "github.com/conductorone/baton-eks/pkg/connector"

	cfg "github.com/conductorone/baton-eks/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := config.DefineConfiguration(
		ctx,
		"baton-eks",
		getConnector[*cfg.Eks],
		cfg.Config,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd.Version = version

	err = cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func getConnector[T field.Configurable](ctx context.Context, config T) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	if err := field.Validate(cfg.Config, config); err != nil {
		return nil, err
	}

	// Extract the configuration values
	accessKey := config.GetString(cfg.AccessKeyIdField.FieldName)
	secretKey := config.GetString(cfg.SecretAccessKeyField.FieldName)
	region := config.GetString(cfg.RegionField.FieldName)
	assumeRole := config.GetString(cfg.AssumeRoleArnField.FieldName)
	clusterName := config.GetString(cfg.ClusterNameField.FieldName)

	eksConnector, err := eksCon.New(ctx, accessKey, secretKey, region, assumeRole, clusterName)
	if err != nil {
		l.Error("error creating EKS connector", zap.Error(err))
		return nil, err
	}
	connector, err := connectorbuilder.NewConnector(ctx, eksConnector)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}
	return connector, nil
}
