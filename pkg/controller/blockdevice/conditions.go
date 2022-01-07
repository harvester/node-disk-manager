package blockdevice

import (
	corev1 "k8s.io/api/core/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
)

func setDevicePartitionedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DevicePartitioned.SetStatus(device, string(status))
	diskv1.DevicePartitioned.Message(device, message)
}

func setDeviceFormattedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceFormatted.SetStatus(device, string(status))
	diskv1.DeviceFormatted.Message(device, message)
}

func setDeviceMountedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceMounted.SetStatus(device, string(status))
	diskv1.DeviceMounted.Message(device, message)
}

func setDeviceProvisionedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceProvisioned.SetStatus(device, string(status))
	diskv1.DeviceProvisioned.Message(device, message)
}

func setDeviceFailedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceFailed.SetStatus(device, string(status))
	diskv1.DeviceFailed.Message(device, message)
}

func setDeviceAutoProvisionDetectedCondition(device *diskv1.BlockDevice, status corev1.ConditionStatus, message string) {
	diskv1.DeviceAutoProvisionDetected.SetStatus(device, string(status))
	diskv1.DeviceAutoProvisionDetected.Message(device, message)
}
