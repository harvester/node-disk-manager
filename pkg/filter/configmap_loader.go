package filter

import (
	"context"
	"fmt"
	"os"
	"strings"

	k8scorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Hostname    string            `yaml:"hostname"`
	Devices     []string          `yaml:"devices,omitempty"`
	Provisioner string            `yaml:"provisioner,omitempty"`
	Params      map[string]string `yaml:"params,omitempty"`
}

// ConfigMapLoader loads filter configurations from ConfigMap
type ConfigMapLoader struct {
	configMapClient k8scorev1.ConfigMapClient
	namespace       string
	nodeName        string
	// Fallback values from environment variables (used when ConfigMap is not available or empty)
	envVendorFilter        string
	envPathFilter          string
	envLabelFilter         string
	envAutoProvisionFilter string
}

// NewConfigMapLoader creates a new ConfigMapLoader
func NewConfigMapLoader(configMapClient k8scorev1.ConfigMapClient, namespace, nodeName string, envVendorFilter, envPathFilter, envLabelFilter, envAutoProvisionFilter string) *ConfigMapLoader {
	return &ConfigMapLoader{
		configMapClient:        configMapClient,
		namespace:              namespace,
		nodeName:               nodeName,
		envVendorFilter:        envVendorFilter,
		envPathFilter:          envPathFilter,
		envLabelFilter:         envLabelFilter,
		envAutoProvisionFilter: envAutoProvisionFilter,
	}
}

// GetEnvFilters returns the fallback environment variable values for filters
func (c *ConfigMapLoader) GetEnvFilters() (vendorFilter, pathFilter, labelFilter string) {
	return c.envVendorFilter, c.envPathFilter, c.envLabelFilter
}

// GetEnvAutoProvisionFilter returns the fallback environment variable value for auto-provision filter
func (c *ConfigMapLoader) GetEnvAutoProvisionFilter() string {
	return c.envAutoProvisionFilter
}

// LoadFiltersFromConfigMap loads filter configurations from ConfigMap
// Returns the merged filter strings for the current node, or empty strings if ConfigMap doesn't exist
func (c *ConfigMapLoader) LoadFiltersFromConfigMap(ctx context.Context) (vendorFilter, pathFilter, labelFilter string, err error) {
	logrus.Debug("Attempting to load filter configuration from ConfigMap")

	configMap, err := c.configMapClient.Get(c.namespace, DefaultConfigMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logrus.Infof("ConfigMap %s/%s not found, will fallback to environment variables", c.namespace, DefaultConfigMapName)
			return "", "", "", nil
		}
		return "", "", "", fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Parse filters.yaml
	filtersYAML, exists := configMap.Data[FiltersConfigKey]
	if !exists {
		logrus.Warnf("ConfigMap %s/%s exists but missing %s key, will fallback to environment variables", c.namespace, DefaultConfigMapName, FiltersConfigKey)
		return "", "", "", nil
	}

	filterConfigs, err := c.parseFilterConfigs(filtersYAML)
	if err != nil {
		logrus.Errorf("Failed to parse %s from ConfigMap: %v, will fallback to environment variables", FiltersConfigKey, err)
		return "", "", "", nil
	}

	// Merge configurations: global ("*") + node-specific
	vendorFilter, pathFilter, labelFilter = c.mergeFilterConfigs(filterConfigs)

	logrus.Infof("Successfully loaded filter configuration from ConfigMap for node %s", c.nodeName)
	logrus.Debugf("  - ExcludeVendors: %s", vendorFilter)
	logrus.Debugf("  - ExcludePaths: %s", pathFilter)
	logrus.Debugf("  - ExcludeLabels: %s", labelFilter)

	return vendorFilter, pathFilter, labelFilter, nil
}

// LoadAutoProvisionFromConfigMap loads auto-provision configurations from ConfigMap
// Returns the merged device paths string for the current node, or empty string if ConfigMap doesn't exist
func (c *ConfigMapLoader) LoadAutoProvisionFromConfigMap(ctx context.Context) (devPaths string, err error) {
	logrus.Debug("Attempting to load auto-provision configuration from ConfigMap")

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
		logrus.Debugf("ConfigMap %s/%s exists but missing %s key, auto-provision not configured", c.namespace, DefaultConfigMapName, AutoProvisionConfigKey)
		return "", nil
	}

	autoProvConfigs, err := c.parseAutoProvisionConfigs(autoProvYAML)
	if err != nil {
		logrus.Errorf("Failed to parse %s from ConfigMap: %v, will fallback to environment variables", AutoProvisionConfigKey, err)
		return "", nil
	}

	// Merge configurations: global ("*") + node-specific
	devPaths = c.mergeAutoProvisionConfigs(autoProvConfigs)

	logrus.Infof("Successfully loaded auto-provision configuration from ConfigMap for node %s", c.nodeName)
	logrus.Debugf("  - Devices: %s", devPaths)

	return devPaths, nil
}

// parseFilterConfigs parses the filters YAML content
func (c *ConfigMapLoader) parseFilterConfigs(yamlContent string) ([]FilterConfig, error) {
	var configs []FilterConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal filters YAML: %w", err)
	}
	return configs, nil
}

// parseAutoProvisionConfigs parses the auto-provision YAML content
// If provisioner is empty, defaults to "longhornv1"
func (c *ConfigMapLoader) parseAutoProvisionConfigs(yamlContent string) ([]AutoProvisionConfig, error) {
	var configs []AutoProvisionConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal autoprovision YAML: %w", err)
	}

	// Set default provisioner if not specified
	for i := range configs {
		if configs[i].Provisioner == "" {
			configs[i].Provisioner = "longhornv1"
			logrus.Debugf("Auto-provision config for hostname '%s' has no provisioner specified, defaulting to 'longhornv1'", configs[i].Hostname)
		}
	}

	return configs, nil
}

// mergeFilterConfigs merges global and node-specific filter configurations
// Priority: node-specific > global
func (c *ConfigMapLoader) mergeFilterConfigs(configs []FilterConfig) (vendorFilter, pathFilter, labelFilter string) {
	var vendors, paths, labels []string

	// First, collect global rules (hostname: "*" or empty)
	for _, config := range configs {
		if c.matchesHostname(config.Hostname, c.nodeName) && (config.Hostname == "*" || config.Hostname == "") {
			vendors = append(vendors, config.ExcludeVendors...)
			paths = append(paths, config.ExcludePaths...)
			labels = append(labels, config.ExcludeLabels...)
		}
	}

	// Then, collect node-specific rules (higher priority)
	for _, config := range configs {
		if c.matchesHostname(config.Hostname, c.nodeName) && config.Hostname != "*" && config.Hostname != "" {
			vendors = append(vendors, config.ExcludeVendors...)
			paths = append(paths, config.ExcludePaths...)
			labels = append(labels, config.ExcludeLabels...)
		}
	}

	return strings.Join(vendors, ","), strings.Join(paths, ","), strings.Join(labels, ",")
}

// mergeAutoProvisionConfigs merges global and node-specific auto-provision configurations
// Priority: node-specific > global
func (c *ConfigMapLoader) mergeAutoProvisionConfigs(configs []AutoProvisionConfig) string {
	var devices []string

	// First, collect global rules (hostname: "*" or empty)
	for _, config := range configs {
		if c.matchesHostname(config.Hostname, c.nodeName) && (config.Hostname == "*" || config.Hostname == "") {
			devices = append(devices, config.Devices...)
		}
	}

	// Then, collect node-specific rules (higher priority)
	for _, config := range configs {
		if c.matchesHostname(config.Hostname, c.nodeName) && config.Hostname != "*" && config.Hostname != "" {
			devices = append(devices, config.Devices...)
		}
	}

	return strings.Join(devices, ",")
}

// matchesHostname checks if the hostname pattern matches the node name
// Supports wildcard "*", empty string (both treated as global), and exact match
func (c *ConfigMapLoader) matchesHostname(pattern, nodeName string) bool {
	if pattern == "*" || pattern == "" {
		return true
	}
	// TODO: Add glob pattern matching support in future phases
	// For now, only support exact match, "*", and empty string
	return pattern == nodeName
}

// Helper function for testing - allows reading ConfigMap name from env
func getConfigMapName() string {
	if name := os.Getenv("NDM_CONFIGMAP_NAME"); name != "" {
		return name
	}
	return DefaultConfigMapName
}

// Helper function for testing - allows reading ConfigMap namespace from env
func getConfigMapNamespace() string {
	if ns := os.Getenv("NDM_CONFIGMAP_NAMESPACE"); ns != "" {
		return ns
	}
	return DefaultConfigMapNamespace
}
