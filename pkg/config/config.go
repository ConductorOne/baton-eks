package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	// Add the SchemaFields for the Config.
	ExternalIdField = field.StringField(
		"external-id",
		field.WithDisplayName("External ID"),
		field.WithDescription("The external id for the aws account"),
	)
	GlobalAccessKeyIdField = field.StringField(
		"global-access-key-id",
		field.WithDisplayName("Global Access Key"),
		field.WithDescription("The global-access-key-id for the aws account"),
	)
	GlobalBindingExternalIdField = field.StringField(
		"global-binding-external-id",
		field.WithDisplayName("Global Binding External ID"),
		field.WithDescription("The global external id for the aws account"),
	)
	GlobalRegionField = field.StringField(
		"global-region",
		field.WithDisplayName("Global Region"),
		field.WithDescription("The region for the aws account"),
	)
	GlobalRoleArnField = field.StringField(
		"global-role-arn",
		field.WithDisplayName("Global Role ARN"),
		field.WithDescription("The role arn for the aws account"),
	)
	GlobalSecretAccessKeyField = field.StringField(
		"global-secret-access-key",
		field.WithDisplayName("Global Secret Access Key"),
		field.WithDescription("The global-secret-access-key for the aws account"),
	)
	RoleArnField = field.StringField(
		"role-arn",
		field.WithDescription("The ARN of the role to assume for the connector"),
		field.WithRequired(true),
		field.WithDisplayName("Assume role ARN"),
	)
	ClusterNameField = field.StringField(
		"eks-cluster-name",
		field.WithRequired(true),
		field.WithDescription("The name of the EKS cluster to connect to"),
		field.WithDisplayName("Cluster name"),
	)
	RegionField = field.StringField(
		"eks-region",
		field.WithRequired(true),
		field.WithDescription("The region for the aws account"),
		field.WithDisplayName("Region"),
	)
	AccessKeyIdField = field.StringField(
		"eks-access-key",
		field.WithDescription("The access-key-id for the aws user"),
		field.WithDisplayName("Access key"),
		field.WithIsSecret(true),
	)
	SecretAccessKeyField = field.StringField(
		"eks-secret-access-key",
		field.WithDescription("The secret-access-key for the aws user"),
		field.WithDisplayName("Secret access key"),
		field.WithIsSecret(true),
	)

	ConfigurationFields = []field.SchemaField{
		ExternalIdField,
		GlobalAccessKeyIdField,
		GlobalBindingExternalIdField,
		GlobalRegionField,
		GlobalRoleArnField,
		GlobalSecretAccessKeyField,
		RoleArnField,
		AccessKeyIdField,
		SecretAccessKeyField,
		RegionField,
		ClusterNameField,
	}

	FieldRelationships = []field.SchemaFieldRelationship{
		field.FieldsRequiredTogether(AccessKeyIdField, SecretAccessKeyField),
		field.FieldsMutuallyExclusive(AccessKeyIdField, GlobalAccessKeyIdField),
		field.FieldsMutuallyExclusive(SecretAccessKeyField, GlobalSecretAccessKeyField),
	}
)

//go:generate go run -tags=generate ./gen
var Config = field.NewConfiguration(
	ConfigurationFields,
	field.WithConstraints(FieldRelationships...),
	field.WithConnectorDisplayName("EKS"),
)
