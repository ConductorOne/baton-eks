package config

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/stretchr/testify/assert"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Eks
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Eks{
				EksAccessKey:       "MYACCESSKEY01",
				EksSecretAccessKey: "secretacesskey010203",
				EksRegion:          "us-east-1",
				EksClusterName:     "my-cluster",
				RoleArn:            "arn:aws:iam::1234567891012:role/MyRole",
			},
			wantErr: false,
		},
		{
			name:    "invalid config - missing required fields",
			config:  &Eks{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := field.Validate(Config, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
