package v1beta1

import (
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/rancher/wrangler/pkg/condition"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	DeviceMounted condition.Cond = "Mounted"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=bd,scope=Namespaced
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=`.status.deviceStatus.details.deviceType`
// +kubebuilder:printcolumn:name="MountPoint",type="string",JSONPath=`.status.deviceStatus.fileSystem.mountPoint`
// +kubebuilder:printcolumn:name="NodeName",type="string",JSONPath=`.spec.nodeName`
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

type BlockDevice struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              BlockDeviceSpec   `json:"spec"`
	Status            BlockDeviceStatus `json:"status"`
}

type BlockDeviceSpec struct {
	// a Node struct, describe the node details the BD is attached to
	// +kubebuilder:validation:Required
	NodeName string `json:"nodeName"`

	// a string with the device path of the disk, e.g. "/dev/sda1"
	// +kubebuilder:validation:Required
	DevPath string `json:"devPath"`

	// +optional
	FileSystem FilesystemInfo `json:"fileSystem,omitempty"`
}

type BlockDeviceStatus struct {
	// the current state of the block device, options are "Active", "Inactive", or "Unknown"
	// +kubebuilder:validation:Enum:=Active;Inactive;Unknown
	State BlockDeviceState `json:"state"`

	// +optional
	Conditions []Condition `json:"conditions,omitempty"`

	// +optional
	DeviceStatus DeviceStatus `json:"deviceStatus,omitempty"`
}

type FilesystemInfo struct {
	// a string with the partition's mount point, or "" if no mount point was discovered
	MountPoint string `json:"mountPoint"`

	// a bool indicating the device is force formatted to overwrite the existing one
	ForceFormatted bool `json:"forceFormatted,omitempty"`
}

type DeviceStatus struct {
	// a string with the parent device path of the disk, e.g. "/dev/sda"
	// e.g `/dev/sda` is the parent for `/dev/sda1`
	ParentDevice string `json:"parentDevice,omitempty"`

	// a bool indicating if the disk is partitioned
	Partitioned bool `json:"partitioned"`

	// a object describe the disk capacity
	Capacity DeviceCapcity `json:"capacity"`

	// a object describe the disk details
	Details DeviceDetails `json:"details"`

	FileSystem FilesystemStatus `json:"fileSystem"`
}

type DeviceCapcity struct {
	// the amount of storage the disk provides
	SizeBytes uint64 `json:"sizeBytes"`

	// the size of the physical blocks used on the disk, in bytes
	PhysicalBlockSizeBytes uint64 `json:"physicalBlockSizeBytes"`
}

type DeviceDetails struct {
	// a string represents the type of the device, options are "disk", "part"
	// +kubebuilder:validation:Enum:=disk;part
	DeviceType BlockDeviceType `json:"deviceType"`

	// a string represents the type of drive bus, options are "HDD", "FDD", "ODD", or "SSD",
	// which correspond to a hard disk drive (rotational), floppy drive, optical (CD/DVD) drive and solid-state drive
	// +kubebuilder:validation:Enum:=HDD;FDD;ODD;SSD;Unknown
	DriveType string `json:"driveType"`

	// PartUUID is a partition-table-level UUID for the partition, a standard feature for all partitions on GPT-partitioned disks
	PartUUID string `json:"partUUID,omitempty"`

	// UUID is a filesystem-level UUID, which is retrieved from the filesystem metadata inside the partition
	// This would be volume UUID on macOS, PartUUID on linux, empty on Windows
	UUID string `json:"uuid,omitempty"`

	// PtUUID is the UUID of the partition table itself, a unique identifier for the entire disk assigned at the time the disk was partitioned
	PtUUID string `json:"ptUUID,omitempty"`

	// contains a boolean indicating if the disk drive is removable
	IsRemovable bool `json:"isRemovable,omitempty"`

	// the type of storage controller/drive, options are "SCSI", "IDE", "virtio", "MMC", or "NVMe"
	// +kubebuilder:validation:Enum:=SCSI;IDE;virtio;MMC;NVMe;Unknown
	StorageController string `json:"storageController"`

	// a string represents the block device bus path
	BusPath string `json:"busPath,omitempty"`

	// a string with the vendor-assigned disk model name
	Model string `json:"model,omitempty"`

	// a string with the name of the hardware vendor for the disk drive
	Vendor string `json:"vendor,omitempty"`

	// a string with the disk's serial number
	SerialNumber string `json:"serialNumber,omitempty"`

	// the numeric index of the NUMA node this disk is local to, or -1
	NUMANodeID int `json:"numaNodeID,omitempty"`

	// a string with the disk's World Wide Name(WWN)
	WWN string `json:"wwn,omitempty"`

	// a string containing the disk label
	Label string `json:"label,omitempty"`
}

type FilesystemStatus struct {
	// a bool indicating the partition is read-only
	IsReadOnly bool `json:"isReadOnly,omitempty"`

	// a string indicated the filesystem type for the partition, or "" if the system could not determine the type.
	Type string `json:"type"`

	// a string with the partition's mount point, or "" if no mount point was discovered
	MountPoint string `json:"mountPoint"`

	// the last force formatted timestamp, only exist when user operate device formatting through the CRD controller
	LastFormattedAt *metav1.Time `json:"LastFormattedAt,omitempty"`
}

type StorageController string

const (
	// StorageControllerIDE is the type of storage controller, IDE stands for Integrated Drive Electronics
	StorageControllerIDE StorageController = "IDE"
	// StorageControllerSCSI is the type of storage controller, SCSI stands for Small Computer System Interface
	StorageControllerSCSI StorageController = "SCSI"
	// StorageControllerNVMe is the type of storage controller, NVMe stands for Non-Volatile Memory express
	StorageControllerNVMe StorageController = "NVMe"
	// StorageControllerVirtio is the type of storage controller, virtio is a virtualization standard for network and disk device drivers
	StorageControllerVirtio StorageController = "virtio"
	// StorageControllerMMC is the type of storage controller, MMC stands for Multi Media Card
	StorageControllerMMC StorageController = "MMC"
)

type DriveType string

const (
	// DriveTypeHDD is the type of drive, which correspond to a hard disk drive (rotational)
	DriveTypeHDD DriveType = "HDD"
	// DriveTypeFDD is the type of drive, which correspond to floppy drive
	DriveTypeFDD DriveType = "FDD"
	// DriveTypeODD is the type of drive, which correspond to  optical (CD/DVD) drive
	DriveTypeODD DriveType = "ODD"
	// DriveTypeSSD is the type of drive, which correspond to solid-state drive
	DriveTypeSSD DriveType = "SSD"
)

type BlockDeviceState string

const (
	// BlockDeviceActive is the state for a block device that is connected to the node
	BlockDeviceActive BlockDeviceState = "Active"

	// BlockDeviceInactive is the state for a block device that is disconnected from a node
	BlockDeviceInactive BlockDeviceState = "Inactive"

	// BlockDeviceUnknown is the state for a block device that cannot be determined at this time
	BlockDeviceUnknown BlockDeviceState = "Unknown"
)

type BlockDeviceType string

const (
	// DeviceTypeDisk indicates the device type is disk
	DeviceTypeDisk BlockDeviceType = "disk"

	// DeviceTypePart indicates the device type is partition
	DeviceTypePart BlockDeviceType = "part"
)

type Condition struct {
	// Type of the condition.
	Type condition.Cond `json:"type"`

	// Status of the condition, one of True, False, Unknown.
	Status v1.ConditionStatus `json:"status"`

	// The last time this condition was updated.
	LastUpdateTime string `json:"lastUpdateTime,omitempty"`

	// Last time the condition transitioned from one status to another.
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`

	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`

	// Human-readable message indicating details about last transition
	Message string `json:"message,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Node struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              lhtypes.NodeSpec   `json:"spec"`
	Status            lhtypes.NodeStatus `json:"status"`
}
