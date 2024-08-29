package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConditionType string
type VGDesireState string
type VGStatus string
type VGType string

const (
	// VGStatusActive means the volume group is active
	VGStatusActive VGStatus = "Active"
	// VGStatusInactive means the volume group is inactive
	VGStatusInactive VGStatus = "Inactive"
	// VGStatusUnknown means the volume group is unknown
	VGStatusUnknown VGStatus = "Unknown"

	// VGStateEnabled means the volume group is enabled
	VGStateEnabled VGDesireState = "Enabled"
	// VGStateDisabled means the volume group is disabled
	VGStateDisabled VGDesireState = "Disabled"
	// VGStateReconciling means the volume group is reconciling
	VGStateReconciling VGDesireState = "Reconciling"

	// VGConditionReady means the volume group is ready
	VGConditionReady ConditionType = "Ready"
	// VGConditionAddDevice means the volume group is added a device
	VGConditionAddDevice ConditionType = "AddDevice"

	// ConditionTypeActive indicates the volume group is active
	ConditionTypeActive ConditionType = "Active"
	// ConditionTypeInactive indicates the volume group is inactive
	ConditionTypeInactive ConditionType = "Inactive"
	// ConditionTypeReconciling indicates the new device is added to the volume group
	ConditionTypeDeviceAdded ConditionType = "DeviceAdded"
	// ConditionTypeEndpointChanged indicates the device is removed from the volume group
	ConditionTypeDeviceRemoved ConditionType = "DeviceRemoved"

	// VGTypeStripe indicates the volume group is stripe
	VGTypeStripe VGType = "stripe"
	// VGTypeDMThin indicates the volume group is dm-thin
	VGTypeDMThin VGType = "dm-thin"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=lvmvg;lvmvgs,scope=Namespaced
// +kubebuilder:printcolumn:name="Parameters",type="string",JSONPath=`.spec.parameters`
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=`.status.vgStatus`
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=`.spec.nodeName`
// +kubebuilder:subresource:status

type LVMVolumeGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              VolumeGroupSpec    `json:"spec"`
	Status            *VolumeGroupStatus `json:"status,omitempty"`
}

type VolumeGroupSpec struct {
	// VGName is the name of the volume group
	// +kubebuilder:validation:Required
	VgName string `json:"vgName"`

	// NodeName is the name of the node where the volume group is created
	// +kubebuilder:validation:Required
	NodeName string `json:"nodeName"`

	// DesiredState is the desired state of the volume group
	// enabled means we will keep this vg active, disabled means we will keep this vg inactive
	// +kubebuilder:validation:Enum:=Enabled;Disabled
	DesiredState VGDesireState `json:"desiredState"`

	// The devices of the volume group
	// format: map[<bd Name>]=devPath"
	// e.g. map[087fc9702c450bfca5ba56b06ba7d7f2] = /dev/sda
	// +kubebuilder:validation:Optional
	Devices map[string]string `json:"devices,omitempty"`

	// Parameters is the parameters for creating the volume group *optional*
	// +kubebuilder:validation:Optional
	Parameters string `json:"parameters,omitempty"`
}

type VolumeGroupStatus struct {

	// The conditions of the volume group
	// +kubebuilder:validation:Optional
	// +Kubebuilder:default={}
	VGConditions []VolumeGroupCondition `json:"conditions,omitempty"`

	// The devices of the volume group
	// format: map[<bd Name>]=devPath"
	// +kubebuilder:validation:Optional
	Devices map[string]string `json:"devices,omitempty"`

	// Parameters is the current parameters of the volume group
	// +kubebuilder:validation:Optional
	Parameters string `json:"parameters,omitempty"`

	// VGTargetType is the target type of the volume group, now only support stripe/dm-thin
	// +kubebuilder:validation:Eum:=stripe;dm-thin
	VGTargetType VGType `json:"vgTargetType,omitempty"`

	// The status of the volume group
	// +kubebuilder:validation:Enum:=Active;Inactive;Unknown
	// +kubebuilder:default:=Unknown
	Status VGStatus `json:"vgStatus,omitempty"`
}

type VolumeGroupCondition struct {
	Type               ConditionType          `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}
