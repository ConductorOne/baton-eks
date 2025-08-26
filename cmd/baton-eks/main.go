//go:build !generate

package main

import (
	"context"
	"fmt"
	"os"

	eksCon "github.com/conductorone/baton-eks/pkg/connector"

	"github.com/conductorone/baton-eks/pkg/config"
	sdkCfg "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := sdkCfg.DefineConfiguration(
		ctx,
		"baton-eks",
		getConnector[*config.Eks],
		config.Config,
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

func getConnector[T field.Configurable](ctx context.Context, cfg *config.Eks) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	if err := field.Validate(config.Config, cfg); err != nil {
		return nil, err
	}

	eksConnector, err := eksCon.New(ctx, cfg)
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
