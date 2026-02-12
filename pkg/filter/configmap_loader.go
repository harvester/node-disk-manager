package filter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	k8scorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/harvester/node-disk-manager/pkg/provisioner"
)

const (
	DefaultConfigMapName      = "harvester-node-disk-manager"
	DefaultConfigMapNamespace = "harvester-system"
	FiltersConfigKey          = "filters.yaml"
	AutoProvisionConfigKey    = "autoprovision.yaml"
)

// FilterConfig represents a single filter configuration block
type FilterConfig struct {
	Hostname       string   `yaml:"hostname"`
	ExcludeDevices []string `yaml:"excludeDevices,omitempty"`
	ExcludeLabels  []string `yaml:"excludeLabels,omitempty"`
	ExcludeVendors []string `yaml:"excludeVendors,omitempty"`
	ExcludePaths   []string `yaml:"excludePaths,omitempty"`
}

// AutoProvisionConfig represents a single auto-provision configuration block
type AutoProvisionConfig struct {
	Hostname string   `yaml:"hostname"`
	Devices  []string `yaml:"devices,omitempty"`

	// We haven't support auto provision for longhorn v2 and LVM yet.
	// But, we keep the provisioner and params fields for future extension.
	Provisioner string            `yaml:"provisioner,omitempty"`
	Params      map[string]string `yaml:"params,omitempty"`
}

// ConfigMapLoader loads filter configurations from ConfigMap
type ConfigMapLoader struct {
	configMapClient k8scorev1.ConfigMapClient
	namespace       string
	configMapName   string
	nodeName        string
	// Fallback values from environment variables (used when ConfigMap is not available or empty)
	envVendorFilter        string
	envPathFilter          string
	envLabelFilter         string
	envAutoProvisionFilter string
}

// NewConfigMapLoader creates a new ConfigMapLoader
func NewConfigMapLoader(configMapClient k8scorev1.ConfigMapClient, nodeName string, envVendorFilter, envPathFilter, envLabelFilter, envAutoProvisionFilter string) *ConfigMapLoader {
	return &ConfigMapLoader{
		configMapClient:        configMapClient,
		namespace:              DefaultConfigMapNamespace,
		configMapName:          DefaultConfigMapName,
		nodeName:               nodeName,
		envVendorFilter:        envVendorFilter,
		envPathFilter:          envPathFilter,
		envLabelFilter:         envLabelFilter,
		envAutoProvisionFilter: envAutoProvisionFilter,
	}
}

// GetEnvFilters returns the fallback environment variable values for filters
// deviceFilter is always empty as it's a new feature only available via ConfigMap
func (c *ConfigMapLoader) GetEnvFilters() (deviceFilter, vendorFilter, pathFilter, labelFilter string) {
	return "", c.envVendorFilter, c.envPathFilter, c.envLabelFilter
}

// GetEnvAutoProvisionFilter returns the fallback environment variable value for auto-provision filter
func (c *ConfigMapLoader) GetEnvAutoProvisionFilter() string {
	return c.envAutoProvisionFilter
}

// LoadFiltersFromConfigMap loads filter configurations from ConfigMap
// Returns the merged filter strings for the current node, or empty strings if ConfigMap doesn't exist
func (c *ConfigMapLoader) LoadFiltersFromConfigMap(ctx context.Context) (deviceFilter, vendorFilter, pathFilter, labelFilter string, err error) {
	logrus.Debug("Attempting to load filter configuration from ConfigMap")

	configMap, err := c.configMapClient.Get(c.namespace, c.configMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logrus.Infof("ConfigMap %s/%s not found, will fallback to environment variables", c.namespace, c.configMapName)
			return "", "", "", "", nil
		}
		return "", "", "", "", fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Parse filters.yaml
	filtersYAML, exists := configMap.Data[FiltersConfigKey]
	if !exists {
		logrus.Warnf("ConfigMap %s/%s exists but missing %s key, will fallback to environment variables", c.namespace, c.configMapName, FiltersConfigKey)
		return "", "", "", "", nil
	}

	filterConfigs, err := c.ParseFilterConfigs(filtersYAML)
	if err != nil {
		logrus.Errorf("Failed to parse %s from ConfigMap: %v, will fallback to environment variables", FiltersConfigKey, err)
		return "", "", "", "", nil
	}

	// Merge configurations: global ("*") + node-specific
	deviceFilter, vendorFilter, pathFilter, labelFilter = c.mergeFilterConfigs(filterConfigs)

	logrus.Infof("Successfully loaded filter configuration from ConfigMap for node %s", c.nodeName)
	logrus.Infof("  - ExcludeDevices: %s", deviceFilter)
	logrus.Infof("  - ExcludeVendors: %s", vendorFilter)
	logrus.Infof("  - ExcludePaths: %s", pathFilter)
	logrus.Infof("  - ExcludeLabels: %s", labelFilter)

	return deviceFilter, vendorFilter, pathFilter, labelFilter, nil
}

// LoadAutoProvisionFromConfigMap loads auto-provision configurations from ConfigMap
// Returns the merged device paths string for the current node, or empty string if ConfigMap doesn't exist
func (c *ConfigMapLoader) LoadAutoProvisionFromConfigMap(ctx context.Context) (devPaths string, err error) {
	logrus.Info("Attempting to load auto-provision configuration from ConfigMap")

	configMap, err := c.configMapClient.Get(c.namespace, DefaultConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logrus.Infof("ConfigMap %s/%s not found, will fallback to environment variables", c.namespace, DefaultConfigMapName)
			return "", nil
		}
		return "", fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Parse autoprovision.yaml
	autoProvYAML, exists := configMap.Data[AutoProvisionConfigKey]
	if !exists {
		logrus.Infof("ConfigMap %s/%s exists but missing %s key, auto-provision not configured", c.namespace, DefaultConfigMapName, AutoProvisionConfigKey)
		return "", nil
	}

	autoProvConfigs, err := c.ParseAutoProvisionConfigs(autoProvYAML)
	if err != nil {
		logrus.Errorf("Failed to parse %s from ConfigMap: %v, will fallback to environment variables", AutoProvisionConfigKey, err)
		return "", nil
	}

	// Merge configurations: global ("*") + node-specific
	devPaths = c.mergeAutoProvisionConfigs(autoProvConfigs)

	logrus.Infof("Successfully loaded auto-provision configuration from ConfigMap for node %s", c.nodeName)
	logrus.Infof("  - Devices: %s", devPaths)

	return devPaths, nil
}

// ParseFilterConfigs parses the filters YAML content
func (c *ConfigMapLoader) ParseFilterConfigs(yamlContent string) ([]FilterConfig, error) {
	var configs []FilterConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal filters YAML: %w", err)
	}
	return configs, nil
}

// ParseAutoProvisionConfigs parses the auto-provision YAML content
// If provisioner is empty, defaults to provisioner.TypeLonghornV1
func (c *ConfigMapLoader) ParseAutoProvisionConfigs(yamlContent string) ([]AutoProvisionConfig, error) {
	var configs []AutoProvisionConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal autoprovision YAML: %w", err)
	}

	// Set default provisioner if not specified
	// Currently this field is not used, but we set it for future extension
	for i := range configs {
		if configs[i].Provisioner == "" {
			configs[i].Provisioner = provisioner.TypeLonghornV1
			logrus.Debugf("Auto-provision config for hostname '%s' has no provisioner specified, defaulting to 'LonghornV1'", configs[i].Hostname)
		}
	}

	return configs, nil
}

// mergeFilterConfigs merges global and node-specific filter configurations
func (c *ConfigMapLoader) mergeFilterConfigs(configs []FilterConfig) (deviceFilter, vendorFilter, pathFilter, labelFilter string) {
	var devices, vendors, paths, labels []string

	for _, config := range configs {
		if c.matchesHostname(config.Hostname, c.nodeName) {
			devices = append(devices, config.ExcludeDevices...)
			vendors = append(vendors, config.ExcludeVendors...)
			paths = append(paths, config.ExcludePaths...)
			labels = append(labels, config.ExcludeLabels...)
		}
	}

	return strings.Join(devices, ","), strings.Join(vendors, ","), strings.Join(paths, ","), strings.Join(labels, ",")
}

// mergeAutoProvisionConfigs merges global and node-specific auto-provision configurations
func (c *ConfigMapLoader) mergeAutoProvisionConfigs(configs []AutoProvisionConfig) string {
	var devices []string

	for _, config := range configs {
		if c.matchesHostname(config.Hostname, c.nodeName) {
			devices = append(devices, config.Devices...)
		}
	}

	return strings.Join(devices, ",")
}

// matchesHostname checks if the hostname pattern matches the node name
// Supports wildcard "*" (global match), glob patterns, and exact match
// Empty string hostname is treated as invalid and ignored
func (c *ConfigMapLoader) matchesHostname(pattern, nodeName string) bool {
	if pattern == "" {
		logrus.Warnf("Empty hostname pattern is not allowed, ignoring this configuration")
		return false
	}

	if pattern == "*" {
		return true
	}

	matched, err := filepath.Match(pattern, nodeName)
	if err != nil {
		logrus.Warnf("Invalid hostname pattern '%s': %v, falling back to exact match", pattern, err)
		// Fall back to exact match if pattern is invalid
		return pattern == nodeName
	}

	return matched
}
