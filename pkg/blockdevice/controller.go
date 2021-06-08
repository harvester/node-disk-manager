package blockdevice

import (
	"context"

	diskv1 "github.com/longhorn/node-disk-manager/pkg/apis/longhorn.io/v1beta1"
	ctldiskv1 "github.com/longhorn/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
)

const blockDeviceHandlerName = "longhorn-block-device-handler"

type Controller struct {
	blockdevices ctldiskv1.BlockDeviceController
}

func Register(
	ctx context.Context,
	blockdevices ctldiskv1.BlockDeviceController) {

	controller := &Controller{
		blockdevices: blockdevices,
	}

	blockdevices.OnChange(ctx, blockDeviceHandlerName, controller.OnDiskChange)
	blockdevices.OnRemove(ctx, blockDeviceHandlerName, controller.OnDiskRemove)
}

func (c *Controller) OnDiskChange(key string, Disk *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	//change logic, return original blockdevice if no changes

	DiskCopy := Disk.DeepCopy()
	//make changes to DiskCopy
	return c.blockdevices.Update(DiskCopy)
}

func (c *Controller) OnDiskRemove(key string, Disk *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	//remove logic, return original blockdevice if no changes

	DiskCopy := Disk.DeepCopy()
	//make changes to DiskCopy
	return c.blockdevices.Update(DiskCopy)
}
