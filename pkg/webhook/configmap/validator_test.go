package configmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/harvester/node-disk-manager/pkg/filter"
)

func TestValidateFiltersYAML(t *testing.T) {
	validator := NewConfigMapValidator()

	tests := []struct {
		name        string
		yamlContent string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid filters with wildcard",
			yamlContent: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
  excludeVendors: ["longhorn"]`,
			expectError: false,
		},
		{
			name: "valid filters with specific hostname",
			yamlContent: `- hostname: "harvester1"
  excludeVendors: ["longhorn"]`,
			expectError: false,
		},
		{
			name: "invalid: empty hostname",
			yamlContent: `- hostname: ""
  excludeVendors: ["longhorn"]`,
			expectError: true,
			errorMsg:    "filter config at index 0 has empty hostname",
		},
		{
			name: "invalid: empty hostname in second config",
			yamlContent: `- hostname: "*"
  excludeVendors: ["longhorn"]
- hostname: ""
  excludeDevices: ["/dev/sda"]`,
			expectError: true,
			errorMsg:    "filter config at index 1 has empty hostname",
		},
		{
			name:        "invalid: malformed YAML",
			yamlContent: `invalid: yaml: content: [[[`,
			expectError: true,
			errorMsg:    "failed to parse YAML",
		},
		{
			name:        "valid: empty content",
			yamlContent: "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateFiltersYAML(tt.yamlContent)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAutoProvisionYAML(t *testing.T) {
	validator := NewConfigMapValidator()

	tests := []struct {
		name        string
		yamlContent string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid autoprovision with wildcard",
			yamlContent: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"`,
			expectError: false,
		},
		{
			name: "valid autoprovision with specific hostname",
			yamlContent: `- hostname: "harvester1"
  devices:
    - "/dev/sdc"`,
			expectError: false,
		},
		{
			name: "invalid: empty hostname",
			yamlContent: `- hostname: ""
  devices:
    - "/dev/sdc"`,
			expectError: true,
			errorMsg:    "autoprovision config at index 0 has empty hostname",
		},
		{
			name: "invalid: empty hostname in second config",
			yamlContent: `- hostname: "*"
  devices:
    - "/dev/sdc"
- hostname: ""
  devices:
    - "/dev/sdd"`,
			expectError: true,
			errorMsg:    "autoprovision config at index 1 has empty hostname",
		},
		{
			name:        "invalid: malformed YAML",
			yamlContent: `invalid: yaml: content: [[[`,
			expectError: true,
			errorMsg:    "failed to parse YAML",
		},
		{
			name:        "valid: empty content",
			yamlContent: "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateAutoProvisionYAML(tt.yamlContent)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateConfigMap(t *testing.T) {
	validator := NewConfigMapValidator()

	tests := []struct {
		name        string
		configMap   *corev1.ConfigMap
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid ConfigMap with both keys",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "harvester-node-disk-manager",
					Namespace: "harvester-system",
				},
				Data: map[string]string{
					filter.FiltersConfigKey: `- hostname: "*"
  excludeVendors: ["longhorn"]`,
					filter.AutoProvisionConfigKey: `- hostname: "*"
  devices: ["/dev/sdc"]`,
				},
			},
			expectError: false,
		},
		{
			name: "invalid filters.yaml with empty hostname",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "harvester-node-disk-manager",
					Namespace: "harvester-system",
				},
				Data: map[string]string{
					filter.FiltersConfigKey: `- hostname: ""
  excludeVendors: ["longhorn"]`,
				},
			},
			expectError: true,
			errorMsg:    "invalid filters.yaml",
		},
		{
			name: "invalid autoprovision.yaml with empty hostname",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "harvester-node-disk-manager",
					Namespace: "harvester-system",
				},
				Data: map[string]string{
					filter.AutoProvisionConfigKey: `- hostname: ""
  devices: ["/dev/sdc"]`,
				},
			},
			expectError: true,
			errorMsg:    "invalid autoprovision.yaml",
		},
		{
			name: "non-target ConfigMap should be ignored",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-other-configmap",
					Namespace: "harvester-system",
				},
				Data: map[string]string{
					"invalid": "should not be validated",
				},
			},
			expectError: false,
		},
		{
			name: "empty data should pass",
			configMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "harvester-node-disk-manager",
					Namespace: "harvester-system",
				},
				Data: map[string]string{},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateConfigMap(tt.configMap)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
