package filter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corefake "k8s.io/client-go/kubernetes/fake"

	fakecleint "github.com/harvester/node-disk-manager/pkg/utils/fake"
)

func TestParseFilterConfigs(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		expectError bool
		expected    []FilterConfig
	}{
		{
			name: "valid global config",
			yamlContent: `- hostname: "*"
  excludeDevices: ["/dev/sda"]
  excludeLabels: ["COS_*"]
  excludeVendors: ["QEMU"]
  excludePaths: ["/mount/path"]`,
			expectError: false,
			expected: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeDevices: []string{"/dev/sda"},
					ExcludeLabels:  []string{"COS_*"},
					ExcludeVendors: []string{"QEMU"},
					ExcludePaths:   []string{"/mount/path"},
				},
			},
		},
		{
			name: "multiple configs",
			yamlContent: `- hostname: "*"
  excludeVendors: ["QEMU"]
- hostname: "node-1"
  excludeDevices: ["/dev/sdb"]`,
			expectError: false,
			expected: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeVendors: []string{"QEMU"},
				},
				{
					Hostname:       "node-1",
					ExcludeDevices: []string{"/dev/sdb"},
				},
			},
		},
		{
			name:        "invalid yaml",
			yamlContent: "invalid: yaml: content:",
			expectError: true,
			expected:    nil,
		},
		{
			name:        "empty yaml",
			yamlContent: "",
			expectError: false,
			expected:    nil,
		},
		{
			name: "omitted hostname key defaults to empty (global)",
			yamlContent: `- excludeDevices: ["/dev/sda"]
  excludeVendors: ["QEMU"]`,
			expectError: false,
			expected: []FilterConfig{
				{
					Hostname:       "", // Should default to empty string
					ExcludeDevices: []string{"/dev/sda"},
					ExcludeVendors: []string{"QEMU"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &ConfigMapLoader{nodeName: "test-node"}
			configs, err := loader.parseFilterConfigs(tt.yamlContent)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, configs)
			}
		})
	}
}

func TestParseAutoProvisionConfigs(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		expectError bool
		expected    []AutoProvisionConfig
	}{
		{
			name: "valid auto-provision config",
			yamlContent: `- hostname: "*"
  devices: ["/dev/nvme*"]
  provisioner: lvm
  params:
    vgName: "harvester-vg"`,
			expectError: false,
			expected: []AutoProvisionConfig{
				{
					Hostname:    "*",
					Devices:     []string{"/dev/nvme*"},
					Provisioner: "lvm",
					Params: map[string]string{
						"vgName": "harvester-vg",
					},
				},
			},
		},
		{
			name: "multiple auto-provision configs",
			yamlContent: `- hostname: "*"
  devices: ["/dev/nvme*"]
  provisioner: lvm
- hostname: "node-gpu-1"
  devices: ["/dev/sd[b-d]"]
  provisioner: longhornv2`,
			expectError: false,
			expected: []AutoProvisionConfig{
				{
					Hostname:    "*",
					Devices:     []string{"/dev/nvme*"},
					Provisioner: "lvm",
				},
				{
					Hostname:    "node-gpu-1",
					Devices:     []string{"/dev/sd[b-d]"},
					Provisioner: "longhornv2",
				},
			},
		},
		{
			name:        "invalid yaml",
			yamlContent: "invalid: yaml: content:",
			expectError: true,
			expected:    nil,
		},
		{
			name: "empty provisioner defaults to longhornv1",
			yamlContent: `- hostname: "*"
  devices: ["/dev/nvme*"]`,
			expectError: false,
			expected: []AutoProvisionConfig{
				{
					Hostname:    "*",
					Devices:     []string{"/dev/nvme*"},
					Provisioner: "longhornv1",
				},
			},
		},
		{
			name: "mixed with and without provisioner",
			yamlContent: `- hostname: "*"
  devices: ["/dev/nvme*"]
  provisioner: lvm
- hostname: "node-1"
  devices: ["/dev/sdb"]`,
			expectError: false,
			expected: []AutoProvisionConfig{
				{
					Hostname:    "*",
					Devices:     []string{"/dev/nvme*"},
					Provisioner: "lvm",
				},
				{
					Hostname:    "node-1",
					Devices:     []string{"/dev/sdb"},
					Provisioner: "longhornv1",
				},
			},
		},
		{
			name: "omitted hostname key defaults to empty (global)",
			yamlContent: `- devices: ["/dev/nvme*"]
  provisioner: lvm`,
			expectError: false,
			expected: []AutoProvisionConfig{
				{
					Hostname:    "", // Should default to empty string
					Devices:     []string{"/dev/nvme*"},
					Provisioner: "lvm",
				},
			},
		},
		{
			name:        "omitted hostname and provisioner (both default)",
			yamlContent: `- devices: ["/dev/sdb"]`,
			expectError: false,
			expected: []AutoProvisionConfig{
				{
					Hostname:    "", // Defaults to empty (global)
					Devices:     []string{"/dev/sdb"},
					Provisioner: "longhornv1", // Defaults to longhornv1
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &ConfigMapLoader{nodeName: "test-node"}
			configs, err := loader.parseAutoProvisionConfigs(tt.yamlContent)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, configs)
			}
		})
	}
}

func TestMergeFilterConfigs(t *testing.T) {
	tests := []struct {
		name           string
		nodeName       string
		configs        []FilterConfig
		expectedDevice string
		expectedVendor string
		expectedPath   string
		expectedLabel  string
	}{
		{
			name:     "global config only",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeVendors: []string{"QEMU", "VMware"},
					ExcludePaths:   []string{"/mount/path"},
					ExcludeLabels:  []string{"COS_*"},
				},
			},
			expectedDevice: "",
			expectedVendor: "QEMU,VMware",
			expectedPath:   "/mount/path",
			expectedLabel:  "COS_*",
		},
		{
			name:     "empty hostname treated as global",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "",
					ExcludeVendors: []string{"QEMU"},
					ExcludePaths:   []string{"/mount/global"},
				},
			},
			expectedDevice: "",
			expectedVendor: "QEMU",
			expectedPath:   "/mount/global",
			expectedLabel:  "",
		},
		{
			name:     "omitted hostname key treated as global",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					// Hostname omitted, defaults to ""
					ExcludeVendors: []string{"VMware"},
					ExcludePaths:   []string{"/mount/default"},
				},
			},
			expectedDevice: "",
			expectedVendor: "VMware",
			expectedPath:   "/mount/default",
			expectedLabel:  "",
		},
		{
			name:     "node-specific overrides global",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeVendors: []string{"QEMU"},
					ExcludePaths:   []string{"/mount/global"},
				},
				{
					Hostname:       "node-1",
					ExcludeVendors: []string{"VMware"},
					ExcludePaths:   []string{"/mount/node1"},
				},
			},
			expectedDevice: "",
			expectedVendor: "QEMU,VMware",
			expectedPath:   "/mount/global,/mount/node1",
			expectedLabel:  "",
		},
		{
			name:     "different node, only global applies",
			nodeName: "node-2",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeVendors: []string{"QEMU"},
				},
				{
					Hostname:       "node-1",
					ExcludeVendors: []string{"VMware"},
				},
			},
			expectedDevice: "",
			expectedVendor: "QEMU",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:           "empty configs",
			nodeName:       "node-1",
			configs:        []FilterConfig{},
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "excludeDevices global config",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeDevices: []string{"/dev/sda", "/dev/sdb"},
				},
			},
			expectedDevice: "/dev/sda,/dev/sdb",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "excludeDevices node-specific overrides global",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeDevices: []string{"/dev/sda"},
				},
				{
					Hostname:       "node-1",
					ExcludeDevices: []string{"/dev/nvme0n1"},
				},
			},
			expectedDevice: "/dev/sda,/dev/nvme0n1",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "excludeDevices with wildcards",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeDevices: []string{"/dev/sd*", "/dev/nvme*"},
				},
			},
			expectedDevice: "/dev/sd*,/dev/nvme*",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "excludeDevices mixed with other filters",
			nodeName: "node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeDevices: []string{"/dev/sda"},
					ExcludeVendors: []string{"QEMU"},
					ExcludePaths:   []string{"/mount/path"},
					ExcludeLabels:  []string{"COS_*"},
				},
			},
			expectedDevice: "/dev/sda",
			expectedVendor: "QEMU",
			expectedPath:   "/mount/path",
			expectedLabel:  "COS_*",
		},
		{
			name:     "excludeDevices different node, only global applies",
			nodeName: "node-2",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeDevices: []string{"/dev/sda"},
				},
				{
					Hostname:       "node-1",
					ExcludeDevices: []string{"/dev/sdb"},
				},
			},
			expectedDevice: "/dev/sda",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "glob pattern prefix match",
			nodeName: "harvester-node-1",
			configs: []FilterConfig{
				{
					Hostname:       "harvester*",
					ExcludeDevices: []string{"/dev/sda"},
					ExcludeVendors: []string{"QEMU"},
				},
			},
			expectedDevice: "/dev/sda",
			expectedVendor: "QEMU",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "glob pattern no match, only global applies",
			nodeName: "worker-node-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeVendors: []string{"VMware"},
				},
				{
					Hostname:       "harvester*",
					ExcludeDevices: []string{"/dev/sda"},
				},
			},
			expectedDevice: "",
			expectedVendor: "VMware",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:     "glob pattern with global and specific",
			nodeName: "node-gpu-1",
			configs: []FilterConfig{
				{
					Hostname:       "*",
					ExcludeVendors: []string{"QEMU"},
				},
				{
					Hostname:       "*-gpu-*",
					ExcludeDevices: []string{"/dev/nvme0n1"},
				},
			},
			expectedDevice: "/dev/nvme0n1",
			expectedVendor: "QEMU",
			expectedPath:   "",
			expectedLabel:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &ConfigMapLoader{nodeName: tt.nodeName}
			device, vendor, path, label := loader.mergeFilterConfigs(tt.configs)

			assert.Equal(t, tt.expectedDevice, device)
			assert.Equal(t, tt.expectedVendor, vendor)
			assert.Equal(t, tt.expectedPath, path)
			assert.Equal(t, tt.expectedLabel, label)
		})
	}
}

func TestMergeAutoProvisionConfigs(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		configs  []AutoProvisionConfig
		expected string
	}{
		{
			name:     "global config only",
			nodeName: "node-1",
			configs: []AutoProvisionConfig{
				{
					Hostname: "*",
					Devices:  []string{"/dev/nvme*", "/dev/sda"},
				},
			},
			expected: "/dev/nvme*,/dev/sda",
		},
		{
			name:     "empty hostname treated as global",
			nodeName: "node-1",
			configs: []AutoProvisionConfig{
				{
					Hostname: "",
					Devices:  []string{"/dev/nvme*"},
				},
			},
			expected: "/dev/nvme*",
		},
		{
			name:     "omitted hostname key treated as global",
			nodeName: "node-1",
			configs: []AutoProvisionConfig{
				{
					// Hostname omitted, defaults to ""
					Devices: []string{"/dev/sdb", "/dev/sdc"},
				},
			},
			expected: "/dev/sdb,/dev/sdc",
		},
		{
			name:     "node-specific adds to global",
			nodeName: "node-1",
			configs: []AutoProvisionConfig{
				{
					Hostname: "*",
					Devices:  []string{"/dev/nvme*"},
				},
				{
					Hostname: "node-1",
					Devices:  []string{"/dev/sdb"},
				},
			},
			expected: "/dev/nvme*,/dev/sdb",
		},
		{
			name:     "different node, only global applies",
			nodeName: "node-2",
			configs: []AutoProvisionConfig{
				{
					Hostname: "*",
					Devices:  []string{"/dev/nvme*"},
				},
				{
					Hostname: "node-1",
					Devices:  []string{"/dev/sdb"},
				},
			},
			expected: "/dev/nvme*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &ConfigMapLoader{nodeName: tt.nodeName}
			result := loader.mergeAutoProvisionConfigs(tt.configs)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesHostname(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		nodeName string
		expected bool
	}{
		{
			name:     "wildcard matches anything",
			pattern:  "*",
			nodeName: "any-node",
			expected: true,
		},
		{
			name:     "empty string matches anything (global)",
			pattern:  "",
			nodeName: "any-node",
			expected: true,
		},
		{
			name:     "exact match",
			pattern:  "node-1",
			nodeName: "node-1",
			expected: true,
		},
		{
			name:     "no match",
			pattern:  "node-1",
			nodeName: "node-2",
			expected: false,
		},
		{
			name:     "glob pattern prefix match",
			pattern:  "harvester*",
			nodeName: "harvester-node-1",
			expected: true,
		},
		{
			name:     "glob pattern prefix no match",
			pattern:  "harvester*",
			nodeName: "worker-node-1",
			expected: false,
		},
		{
			name:     "glob pattern suffix match",
			pattern:  "*-gpu",
			nodeName: "node-gpu",
			expected: true,
		},
		{
			name:     "glob pattern contains match",
			pattern:  "*-gpu-*",
			nodeName: "node-gpu-1",
			expected: true,
		},
		{
			name:     "glob pattern character class match",
			pattern:  "node-[1-3]",
			nodeName: "node-2",
			expected: true,
		},
		{
			name:     "glob pattern character class no match",
			pattern:  "node-[1-3]",
			nodeName: "node-5",
			expected: false,
		},
		{
			name:     "glob pattern question mark match",
			pattern:  "node-?",
			nodeName: "node-1",
			expected: true,
		},
		{
			name:     "glob pattern question mark no match",
			pattern:  "node-?",
			nodeName: "node-10",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &ConfigMapLoader{nodeName: tt.nodeName}
			result := loader.matchesHostname(tt.pattern, tt.nodeName)

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoadFiltersFromConfigMap(t *testing.T) {
	tests := []struct {
		name           string
		configMapData  map[string]string
		nodeName       string
		expectError    bool
		expectedDevice string
		expectedVendor string
		expectedPath   string
		expectedLabel  string
	}{
		{
			name: "valid filters config",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeVendors: ["QEMU"]
  excludePaths: ["/mount/path"]
  excludeLabels: ["COS_*"]`,
			},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "",
			expectedVendor: "QEMU",
			expectedPath:   "/mount/path",
			expectedLabel:  "COS_*",
		},
		{
			name: "node-specific config",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeVendors: ["QEMU"]
- hostname: "node-1"
  excludeVendors: ["VMware"]`,
			},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "",
			expectedVendor: "QEMU,VMware",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name:           "missing filters key",
			configMapData:  map[string]string{},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "invalid yaml",
			configMapData: map[string]string{
				FiltersConfigKey: "invalid: yaml: content:",
			},
			nodeName:       "node-1",
			expectError:    false, // Should not error, just fallback
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "excludeDevices global config",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeDevices: ["/dev/sda", "/dev/sdb"]`,
			},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "/dev/sda,/dev/sdb",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "excludeDevices node-specific config",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeDevices: ["/dev/sda"]
- hostname: "node-1"
  excludeDevices: ["/dev/nvme0n1"]`,
			},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "/dev/sda,/dev/nvme0n1",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "excludeDevices with wildcards",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeDevices: ["/dev/sd*", "/dev/nvme*"]`,
			},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "/dev/sd*,/dev/nvme*",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "excludeDevices mixed with all filters",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeDevices: ["/dev/sda"]
  excludeVendors: ["QEMU"]
  excludePaths: ["/mount/path"]
  excludeLabels: ["COS_*"]`,
			},
			nodeName:       "node-1",
			expectError:    false,
			expectedDevice: "/dev/sda",
			expectedVendor: "QEMU",
			expectedPath:   "/mount/path",
			expectedLabel:  "COS_*",
		},
		{
			name: "glob pattern prefix match",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "harvester*"
  excludeDevices: ["/dev/sda"]
  excludeVendors: ["QEMU"]`,
			},
			nodeName:       "harvester-node-1",
			expectError:    false,
			expectedDevice: "/dev/sda",
			expectedVendor: "QEMU",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "glob pattern no match",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "harvester*"
  excludeDevices: ["/dev/sda"]`,
			},
			nodeName:       "worker-node-1",
			expectError:    false,
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
		},
		{
			name: "glob pattern with global fallback",
			configMapData: map[string]string{
				FiltersConfigKey: `- hostname: "*"
  excludeVendors: ["VMware"]
- hostname: "*-gpu-*"
  excludeDevices: ["/dev/nvme0n1"]`,
			},
			nodeName:       "node-gpu-1",
			expectError:    false,
			expectedDevice: "/dev/nvme0n1",
			expectedVendor: "VMware",
			expectedPath:   "",
			expectedLabel:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClientset := corefake.NewClientset()

			// Add ConfigMap to tracker if data exists
			if len(tt.configMapData) > 0 {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DefaultConfigMapName,
						Namespace: DefaultConfigMapNamespace,
					},
					Data: tt.configMapData,
				}
				err := fakeClientset.Tracker().Add(cm)
				assert.NoError(t, err)
			}

			loader := NewConfigMapLoader(fakecleint.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps), DefaultConfigMapNamespace, tt.nodeName, "", "", "", "")
			device, vendor, path, label, err := loader.LoadFiltersFromConfigMap(context.Background())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDevice, device)
				assert.Equal(t, tt.expectedVendor, vendor)
				assert.Equal(t, tt.expectedPath, path)
				assert.Equal(t, tt.expectedLabel, label)
			}
		})
	}
}

func TestLoadAutoProvisionFromConfigMap(t *testing.T) {
	tests := []struct {
		name            string
		configMapData   map[string]string
		nodeName        string
		expectError     bool
		expectedDevices string
	}{
		{
			name: "valid autoprovision config",
			configMapData: map[string]string{
				AutoProvisionConfigKey: `- hostname: "*"
  devices: ["/dev/nvme*"]`,
			},
			nodeName:        "node-1",
			expectError:     false,
			expectedDevices: "/dev/nvme*",
		},
		{
			name: "node-specific config",
			configMapData: map[string]string{
				AutoProvisionConfigKey: `- hostname: "*"
  devices: ["/dev/nvme*"]
- hostname: "node-1"
  devices: ["/dev/sdb"]`,
			},
			nodeName:        "node-1",
			expectError:     false,
			expectedDevices: "/dev/nvme*,/dev/sdb",
		},
		{
			name:            "missing autoprovision key",
			configMapData:   map[string]string{},
			nodeName:        "node-1",
			expectError:     false,
			expectedDevices: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClientset := corefake.NewClientset()

			// Add ConfigMap to tracker if data exists
			if len(tt.configMapData) > 0 {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DefaultConfigMapName,
						Namespace: DefaultConfigMapNamespace,
					},
					Data: tt.configMapData,
				}
				err := fakeClientset.Tracker().Add(cm)
				assert.NoError(t, err)
			}

			loader := NewConfigMapLoader(fakecleint.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps), DefaultConfigMapNamespace, tt.nodeName, "", "", "", "")
			devices, err := loader.LoadAutoProvisionFromConfigMap(context.Background())

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDevices, devices)
			}
		})
	}
}
