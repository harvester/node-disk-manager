package filter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corefake "k8s.io/client-go/kubernetes/fake"

	fakeclient "github.com/harvester/node-disk-manager/pkg/utils/fake"
)

func TestLoadFiltersFromConfigMap(t *testing.T) {
	tests := []struct {
		name           string
		nodeName       string
		filtersYAML    string
		expectedDevice string
		expectedVendor string
		expectedPath   string
		expectedLabel  string
		expectError    bool
	}{
		// Test 1: filters.yaml is empty
		{
			name:           "empty filters.yaml",
			nodeName:       "harvester1",
			filtersYAML:    "",
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
			expectError:    false,
		},
		// Test 2: filters.yaml contains one hostname: "*" case
		{
			name:     "filters with global wildcard only",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
  excludeVendors: ["longhorn", "thisisaexample"]
  excludeDevices: ["/dev/sdd"]`,
			expectedDevice: "/dev/sdd",
			expectedVendor: "longhorn,thisisaexample",
			expectedPath:   "",
			expectedLabel:  "COS_*,HARV_*",
			expectError:    false,
		},
		// Test 3: filters.yaml contains one hostname: "*" and one hostname: harvester1
		{
			name:     "filters with global and one specific hostname",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
  excludeVendors: ["longhorn", "thisisaexample"]
  excludeDevices: ["/dev/sdd"]
- hostname: "harvester1"
  excludeVendors: ["harvester1"]`,
			expectedDevice: "/dev/sdd",
			expectedVendor: "longhorn,thisisaexample,harvester1",
			expectedPath:   "",
			expectedLabel:  "COS_*,HARV_*",
			expectError:    false,
		},
		// Test 4: filters.yaml contains one hostname: "*" and two hostnames: harvester1 & harvester2
		{
			name:     "filters with global and two specific hostnames",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
  excludeVendors: ["longhorn", "thisisaexample"]
  excludeDevices: ["/dev/sdd"]
- hostname: "harvester1"
  excludeVendors: ["harvester1"]
- hostname: "harvester2"
  excludeVendors: ["harvester2"]`,
			expectedDevice: "/dev/sdd",
			expectedVendor: "longhorn,thisisaexample,harvester1",
			expectedPath:   "",
			expectedLabel:  "COS_*,HARV_*",
			expectError:    false,
		},
		// Test 5: filters.yaml contains one hostname: "*" and two same hostnames: harvester1 & harvester1
		{
			name:     "filters with global and duplicate specific hostname",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
  excludeVendors: ["longhorn", "thisisaexample"]
  excludeDevices: ["/dev/sdd"]
- hostname: "harvester1"
  excludeVendors: ["harvester1"]
- hostname: "harvester1"
  excludeDevices: ["/dev/sde"]`,
			expectedDevice: "/dev/sdd,/dev/sde",
			expectedVendor: "longhorn,thisisaexample,harvester1",
			expectedPath:   "",
			expectedLabel:  "COS_*,HARV_*",
			expectError:    false,
		},
		// Test 6: filters.yaml contains only one hostname: harvester1
		{
			name:     "filters with only specific hostname",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "harvester1"
  excludeVendors: ["harvester1"]`,
			expectedDevice: "",
			expectedVendor: "harvester1",
			expectedPath:   "",
			expectedLabel:  "",
			expectError:    false,
		},
		// Test 7: only excludeLabels
		{
			name:     "filters with only excludeLabels",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]`,
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "COS_*,HARV_*",
			expectError:    false,
		},
		// Test 8: only excludeVendors
		{
			name:     "filters with only excludeVendors",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeVendors: ["longhorn", "thisisaexample"]`,
			expectedDevice: "",
			expectedVendor: "longhorn,thisisaexample",
			expectedPath:   "",
			expectedLabel:  "",
			expectError:    false,
		},
		// Test 9: only excludeDevices
		{
			name:     "filters with only excludeDevices",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeDevices: ["/dev/sdd"]`,
			expectedDevice: "/dev/sdd",
			expectedVendor: "",
			expectedPath:   "",
			expectedLabel:  "",
			expectError:    false,
		},
		// Test 10: only excludePaths
		{
			name:     "filters with only excludePaths",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludePaths: ["/mnt/data", "/var/lib"]`,
			expectedDevice: "",
			expectedVendor: "",
			expectedPath:   "/mnt/data,/var/lib",
			expectedLabel:  "",
			expectError:    false,
		},
		// Test 11: all four filters
		{
			name:     "filters with all four fields",
			nodeName: "harvester1",
			filtersYAML: `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
  excludeVendors: ["longhorn", "thisisaexample"]
  excludeDevices: ["/dev/sdd"]
  excludePaths: ["/mnt/data", "/var/lib"]`,
			expectedDevice: "/dev/sdd",
			expectedVendor: "longhorn,thisisaexample",
			expectedPath:   "/mnt/data,/var/lib",
			expectedLabel:  "COS_*,HARV_*",
			expectError:    false,
		},
		// Test 12: node doesn't match any specific config
		{
			name:     "node doesn't match specific config",
			nodeName: "harvester3",
			filtersYAML: `- hostname: "*"
  excludeVendors: ["longhorn"]
- hostname: "harvester1"
  excludeVendors: ["harvester1"]
- hostname: "harvester2"
  excludeVendors: ["harvester2"]`,
			expectedDevice: "",
			expectedVendor: "longhorn",
			expectedPath:   "",
			expectedLabel:  "",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClientset := corefake.NewClientset()

			// Create ConfigMap if filtersYAML is provided
			if tt.filtersYAML != "" {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DefaultConfigMapName,
						Namespace: DefaultConfigMapNamespace,
					},
					Data: map[string]string{
						FiltersConfigKey: tt.filtersYAML,
					},
				}
				err := fakeClientset.Tracker().Add(cm)
				require.NoError(t, err)
			}

			loader := NewConfigMapLoader(
				fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
				DefaultConfigMapNamespace,
				tt.nodeName,
				"", "", "", "", // env filters
			)

			device, vendor, path, label, err := loader.LoadFiltersFromConfigMap(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDevice, device, "device filter mismatch")
				assert.Equal(t, tt.expectedVendor, vendor, "vendor filter mismatch")
				assert.Equal(t, tt.expectedPath, path, "path filter mismatch")
				assert.Equal(t, tt.expectedLabel, label, "label filter mismatch")
			}
		})
	}
}

func TestLoadAutoProvisionFromConfigMap(t *testing.T) {
	tests := []struct {
		name              string
		nodeName          string
		autoProvisionYAML string
		expectedDevices   string
		expectError       bool
	}{
		// Test 1: autoprovision.yaml is empty
		{
			name:              "empty autoprovision.yaml",
			nodeName:          "harvester1",
			autoProvisionYAML: "",
			expectedDevices:   "",
			expectError:       false,
		},
		// Test 2: autoprovision.yaml contains one hostname: "*" case
		{
			name:     "autoprovision with global wildcard only",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"`,
			expectedDevices: "/dev/sdc,/dev/sdd",
			expectError:     false,
		},
		// Test 3: autoprovision.yaml contains one hostname: "*" and one hostname: harvester1
		{
			name:     "autoprovision with global and one specific hostname",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"
- hostname: "harvester1"
  devices:
    - "/dev/sdf"`,
			expectedDevices: "/dev/sdc,/dev/sdd,/dev/sdf",
			expectError:     false,
		},
		// Test 4: autoprovision.yaml contains one hostname: "*" and two hostnames: harvester1 & harvester2
		{
			name:     "autoprovision with global and two specific hostnames",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"
- hostname: "harvester1"
  devices:
    - "/dev/sdf"
- hostname: "harvester2"
  devices:
    - "/dev/sdg"`,
			expectedDevices: "/dev/sdc,/dev/sdd,/dev/sdf",
			expectError:     false,
		},
		// Test 5: autoprovision.yaml contains one hostname: "*" and two same hostnames: harvester1 & harvester1
		{
			name:     "autoprovision with global and duplicate specific hostname",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"
- hostname: "harvester1"
  devices:
    - "/dev/sdf"
- hostname: "harvester1"
  devices:
    - "/dev/sdh"`,
			expectedDevices: "/dev/sdc,/dev/sdd,/dev/sdf,/dev/sdh",
			expectError:     false,
		},
		// Test 6: autoprovision.yaml contains only one hostname: harvester1
		{
			name:     "autoprovision with only specific hostname",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "harvester1"
  devices:
    - "/dev/sdf"`,
			expectedDevices: "/dev/sdf",
			expectError:     false,
		},
		// Test 7: autoprovision with only devices field
		{
			name:     "autoprovision with only devices",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"`,
			expectedDevices: "/dev/sdc,/dev/sdd",
			expectError:     false,
		},
		// Test 8: autoprovision with devices and provisioner
		{
			name:     "autoprovision with devices and provisioner",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"
  provisioner: lvm`,
			expectedDevices: "/dev/sdc,/dev/sdd",
			expectError:     false,
		},
		// Test 9: autoprovision with devices, provisioner, and params
		{
			name:     "autoprovision with devices, provisioner, and params",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"
  provisioner: lvm
  params:
    vgName: "harvester-vg"`,
			expectedDevices: "/dev/sdc,/dev/sdd",
			expectError:     false,
		},
		// Test 10: autoprovision with wildcard devices
		{
			name:     "autoprovision with wildcard devices",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sd*"
    - "/dev/nvme*"`,
			expectedDevices: "/dev/sd*,/dev/nvme*",
			expectError:     false,
		},
		// Test 11: node doesn't match any specific config
		{
			name:     "node doesn't match specific config",
			nodeName: "harvester3",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
    - "/dev/sdd"
- hostname: "harvester1"
  devices:
    - "/dev/sdf"
- hostname: "harvester2"
  devices:
    - "/dev/sdg"`,
			expectedDevices: "/dev/sdc,/dev/sdd",
			expectError:     false,
		},
		// Test 12: autoprovision with multiple provisioners
		{
			name:     "autoprovision with multiple provisioners",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"
  provisioner: lvm
- hostname: "harvester1"
  devices:
    - "/dev/sdf"
  provisioner: longhornv2`,
			expectedDevices: "/dev/sdc,/dev/sdf",
			expectError:     false,
		},
		// Test 13: autoprovision default provisioner
		{
			name:     "autoprovision with default provisioner",
			nodeName: "harvester1",
			autoProvisionYAML: `- hostname: "*"
  devices:
    - "/dev/sdc"`,
			expectedDevices: "/dev/sdc",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClientset := corefake.NewClientset()

			// Create ConfigMap if autoProvisionYAML is provided
			if tt.autoProvisionYAML != "" {
				cm := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DefaultConfigMapName,
						Namespace: DefaultConfigMapNamespace,
					},
					Data: map[string]string{
						AutoProvisionConfigKey: tt.autoProvisionYAML,
					},
				}
				err := fakeClientset.Tracker().Add(cm)
				require.NoError(t, err)
			}

			loader := NewConfigMapLoader(
				fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
				DefaultConfigMapNamespace,
				tt.nodeName,
				"", "", "", "", // env filters
			)

			devices, err := loader.LoadAutoProvisionFromConfigMap(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedDevices, devices, "devices mismatch")
			}
		})
	}
}

func TestLoadFiltersFromConfigMap_WithMissingConfigMapKey(t *testing.T) {
	ctx := context.Background()
	fakeClientset := corefake.NewClientset()

	// Create ConfigMap without filters.yaml key
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			// filters.yaml key is missing
			"some-other-key": "some-value",
		},
	}
	err := fakeClientset.Tracker().Add(cm)
	require.NoError(t, err)

	loader := NewConfigMapLoader(
		fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
		DefaultConfigMapNamespace,
		"harvester1",
		"", "", "", "",
	)

	device, vendor, path, label, err := loader.LoadFiltersFromConfigMap(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", device)
	assert.Equal(t, "", vendor)
	assert.Equal(t, "", path)
	assert.Equal(t, "", label)
}

func TestLoadAutoProvisionFromConfigMap_WithMissingConfigMapKey(t *testing.T) {
	ctx := context.Background()
	fakeClientset := corefake.NewClientset()

	// Create ConfigMap without autoprovision.yaml key
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			// autoprovision.yaml key is missing
			"some-other-key": "some-value",
		},
	}
	err := fakeClientset.Tracker().Add(cm)
	require.NoError(t, err)

	loader := NewConfigMapLoader(
		fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
		DefaultConfigMapNamespace,
		"harvester1",
		"", "", "", "",
	)

	devices, err := loader.LoadAutoProvisionFromConfigMap(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "", devices)
}

func TestLoadFiltersFromConfigMap_WithInvalidYAML(t *testing.T) {
	ctx := context.Background()
	fakeClientset := corefake.NewClientset()

	// Create ConfigMap with invalid YAML
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			FiltersConfigKey: "invalid: yaml: content: [[[",
		},
	}
	err := fakeClientset.Tracker().Add(cm)
	require.NoError(t, err)

	loader := NewConfigMapLoader(
		fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
		DefaultConfigMapNamespace,
		"harvester1",
		"", "", "", "",
	)

	device, vendor, path, label, err := loader.LoadFiltersFromConfigMap(ctx)
	// Should not error, just return empty strings as fallback
	assert.NoError(t, err)
	assert.Equal(t, "", device)
	assert.Equal(t, "", vendor)
	assert.Equal(t, "", path)
	assert.Equal(t, "", label)
}

func TestLoadAutoProvisionFromConfigMap_WithInvalidYAML(t *testing.T) {
	ctx := context.Background()
	fakeClientset := corefake.NewClientset()

	// Create ConfigMap with invalid YAML
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultConfigMapName,
			Namespace: DefaultConfigMapNamespace,
		},
		Data: map[string]string{
			AutoProvisionConfigKey: "invalid: yaml: content: [[[",
		},
	}
	err := fakeClientset.Tracker().Add(cm)
	require.NoError(t, err)

	loader := NewConfigMapLoader(
		fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
		DefaultConfigMapNamespace,
		"harvester1",
		"", "", "", "",
	)

	devices, err := loader.LoadAutoProvisionFromConfigMap(ctx)
	// Should not error, just return empty string as fallback
	assert.NoError(t, err)
	assert.Equal(t, "", devices)
}

func TestLoadFiltersFromConfigMap_ConfigMapNotFound(t *testing.T) {
	ctx := context.Background()
	fakeClientset := corefake.NewClientset()

	// No ConfigMap added to the tracker

	loader := NewConfigMapLoader(
		fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
		DefaultConfigMapNamespace,
		"harvester1",
		"", "", "", "",
	)

	device, vendor, path, label, err := loader.LoadFiltersFromConfigMap(ctx)
	// Should not error when ConfigMap is not found, just return empty strings
	assert.NoError(t, err)
	assert.Equal(t, "", device)
	assert.Equal(t, "", vendor)
	assert.Equal(t, "", path)
	assert.Equal(t, "", label)
}

func TestLoadAutoProvisionFromConfigMap_ConfigMapNotFound(t *testing.T) {
	ctx := context.Background()
	fakeClientset := corefake.NewClientset()

	// No ConfigMap added to the tracker

	loader := NewConfigMapLoader(
		fakeclient.FakeConfigMapClient(fakeClientset.CoreV1().ConfigMaps),
		DefaultConfigMapNamespace,
		"harvester1",
		"", "", "", "",
	)

	devices, err := loader.LoadAutoProvisionFromConfigMap(ctx)
	// Should not error when ConfigMap is not found, just return empty string
	assert.NoError(t, err)
	assert.Equal(t, "", devices)
}
