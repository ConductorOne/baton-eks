package connector

import (
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
)

// EKS-specific resource types.
var (
	ResourceTypeIAMRole = &v2.ResourceType{
		Id:          "iam_role",
		DisplayName: "IAM Role",
		Description: "AWS IAM Role",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_ROLE},
	}

	ResourceTypeIAMUser = &v2.ResourceType{
		Id:          "iam_user",
		DisplayName: "IAM User",
		Description: "AWS IAM User",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
	}

	ResourceTypeAccessPolicy = &v2.ResourceType{
		Id:          "access_policy",
		DisplayName: "Access Policy",
		Description: "EKS Access Policy",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_ROLE},
	}
)
