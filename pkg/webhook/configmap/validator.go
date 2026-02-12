package configmap

import (
	"fmt"

	werror "github.com/harvester/webhook/pkg/error"
	"github.com/harvester/webhook/pkg/server/admission"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/harvester/node-disk-manager/pkg/filter"
)

const (
	harvesterNodeDiskManagerConfigMap = "harvester-node-disk-manager"
)

type Validator struct {
	admission.DefaultValidator

	loader *filter.ConfigMapLoader
}

func NewConfigMapValidator() *Validator {
	// Create a loader instance for parsing YAML
	// The nil configMapClient and empty strings are fine since we only use the parse methods
	loader := filter.NewConfigMapLoader(nil, "", "", "", "", "", "")
	return &Validator{
		loader: loader,
	}
}

func (v *Validator) Create(_ *admission.Request, newObj runtime.Object) error {
	cm := newObj.(*corev1.ConfigMap)
	return v.validateConfigMap(cm)
}

func (v *Validator) Update(_ *admission.Request, _ runtime.Object, newObj runtime.Object) error {
	cm := newObj.(*corev1.ConfigMap)
	return v.validateConfigMap(cm)
}

func (v *Validator) validateConfigMap(cm *corev1.ConfigMap) error {
	// Only validate the harvester-node-disk-manager ConfigMap
	if cm.Name != harvesterNodeDiskManagerConfigMap {
		return nil
	}

	// Validate filters.yaml if present
	if filtersYAML, exists := cm.Data[filter.FiltersConfigKey]; exists && filtersYAML != "" {
		if err := v.validateFiltersYAML(filtersYAML); err != nil {
			return werror.NewBadRequest(fmt.Sprintf("invalid %s: %v", filter.FiltersConfigKey, err))
		}
	}

	// Validate autoprovision.yaml if present
	if autoProvYAML, exists := cm.Data[filter.AutoProvisionConfigKey]; exists && autoProvYAML != "" {
		if err := v.validateAutoProvisionYAML(autoProvYAML); err != nil {
			return werror.NewBadRequest(fmt.Sprintf("invalid %s: %v", filter.AutoProvisionConfigKey, err))
		}
	}

	return nil
}

// validateFiltersYAML validates the filters.yaml content
// First pass: ensure it can be parsed
// Second pass: ensure no hostname is empty string
func (v *Validator) validateFiltersYAML(yamlContent string) error {
	// First pass: try to parse the YAML
	configs, err := v.loader.ParseFilterConfigs(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Second pass: validate no empty hostname
	for i, config := range configs {
		if config.Hostname == "" {
			return fmt.Errorf("filter config at index %d has empty hostname, which is not allowed", i)
		}
	}

	return nil
}

// validateAutoProvisionYAML validates the autoprovision.yaml content
// First pass: ensure it can be parsed
// Second pass: ensure no hostname is empty string
func (v *Validator) validateAutoProvisionYAML(yamlContent string) error {
	// First pass: try to parse the YAML
	configs, err := v.loader.ParseAutoProvisionConfigs(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Second pass: validate no empty hostname
	for i, config := range configs {
		if config.Hostname == "" {
			return fmt.Errorf("autoprovision config at index %d has empty hostname, which is not allowed", i)
		}
	}

	return nil
}

func (v *Validator) Resource() admission.Resource {
	return admission.Resource{
		Names:      []string{"configmaps"},
		Scope:      admissionregv1.NamespacedScope,
		APIGroup:   corev1.SchemeGroupVersion.Group,
		APIVersion: corev1.SchemeGroupVersion.Version,
		ObjectType: &corev1.ConfigMap{},
		OperationTypes: []admissionregv1.OperationType{
			admissionregv1.Create,
			admissionregv1.Update,
		},
	}
}
