package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

const (
	accessKey     = "eks-access-key"
	accessSecret  = "eks-secret-access-key" // #nosec G101
	region        = "eks-region"
	assumeRoleArn = "eks-assume-role-arn"
	clusterName   = "eks-cluster-name"
)

var (
	// Add the SchemaFields for the Config.
	AccessKeyIdField = field.StringField(
		accessKey,
		field.WithRequired(true),
		field.WithDescription("The access-key-id for the aws account"),
	)
	SecretAccessKeyField = field.StringField(
		accessSecret,
		field.WithRequired(true),
		field.WithDescription("The secret-access-key for the aws account"),
	)
	RegionField = field.StringField(
		region,
		field.WithRequired(true),
		field.WithDescription("The region for the aws account"),
	)
	AssumeRoleArnField = field.StringField(
		assumeRoleArn,
		field.WithDescription("The ARN of the role to assume for the connector."),
	)
	ClusterNameField = field.StringField(
		clusterName,
		field.WithRequired(true),
		field.WithDescription("The name of the EKS cluster to connect to."),
	)

	ConfigurationFields = []field.SchemaField{
		AccessKeyIdField,
		SecretAccessKeyField,
		RegionField,
		AssumeRoleArnField,
		ClusterNameField,
	}

	// FieldRelationships defines relationships between the ConfigurationFields that can be automatically validated.
	// For example, a username and password can be required together, or an access token can be
	// marked as mutually exclusive from the username password pair.
	FieldRelationships = []field.SchemaFieldRelationship{
		field.FieldsRequiredTogether(AccessKeyIdField, SecretAccessKeyField, RegionField),
	}
)

//go:generate go run -tags=generate ./gen
var Config = field.NewConfiguration(
	ConfigurationFields,
	field.WithConstraints(FieldRelationships...),
	field.WithConnectorDisplayName("EKS"),
)
