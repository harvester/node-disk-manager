package indexers

import (
	"fmt"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldisk "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io"
)

const (
	DeviceByPhaseIndex = "ndm.harvesterhci.io/device-by-node"
)

func RegisterIndexers(disk *ctldisk.Factory) {
	bdInformer := disk.Harvesterhci().V1beta1().BlockDevice().Cache()
	bdInformer.AddIndexer(DeviceByPhaseIndex, deviceByPhase)
}

func deviceByPhase(bd *diskv1.BlockDevice) ([]string, error) {
	return []string{MakeDeviceByPhaseKey(bd, bd.Status.ProvisionPhase)}, nil
}

func MakeDeviceByPhaseKey(bd *diskv1.BlockDevice, phase diskv1.BlockDeviceProvisionPhase) string {
	// Must partition the index by nodes
	return fmt.Sprintf("%s.%s", bd.Spec.NodeName, phase)
}
