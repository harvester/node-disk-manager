package blockdevice

import (
	corev1 "k8s.io/api/core/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
)

func setDevicePartitioningCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DevicePartitioning.SetStatus(device, string(status))
	diskv1.DevicePartitioning.Message(device, message)
}

func setDevicePartitionedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DevicePartitioned.SetStatus(device, string(status))
	diskv1.DevicePartitioned.Message(device, message)
}

func setDeviceFormattingCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceFormatting.SetStatus(device, string(status))
	diskv1.DeviceFormatting.Message(device, message)
}

func setDeviceFormattedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceFormatted.SetStatus(device, string(status))
	diskv1.DeviceFormatted.Message(device, message)
}

func setDeviceUnmountingCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceUnmounting.SetStatus(device, string(status))
	diskv1.DeviceUnmounting.Message(device, message)
}

func setDeviceMountingCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceMounting.SetStatus(device, string(status))
	diskv1.DeviceMounting.Message(device, message)
}

func setDeviceMountedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceMounted.SetStatus(device, string(status))
	diskv1.DeviceMounted.Message(device, message)
}

func setDeviceUnprovisioningCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceUnprovisioning.SetStatus(device, string(status))
	diskv1.DeviceUnprovisioning.Message(device, message)
}

func setDeviceProvisionedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceProvisioned.SetStatus(device, string(status))
	diskv1.DeviceProvisioned.Message(device, message)
}
