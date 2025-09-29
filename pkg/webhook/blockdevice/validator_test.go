package blockdevice

import (
	"testing"

	harvv1beta1 "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	ctlharvv1beta1 "github.com/harvester/harvester/pkg/generated/controllers/harvesterhci.io/v1beta1"
	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/utils/fake"
	lhv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	ctlcorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	ctlstoragev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/storage/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdate(t *testing.T) {
	tests := []struct {
		name               string
		blockDeviceToCache []*diskv1.BlockDevice
		scToCache          []*storagev1.StorageClass
		pvToCache          []*v1.PersistentVolume
		volToCache         []*lhv1.Volume
		nodeToCache        []*v1.Node
		vmImageToCache     []*harvv1beta1.VirtualMachineImage
		oldBlockDevice     *diskv1.BlockDevice
		newBlockDeice      *diskv1.BlockDevice
		expectedErr        bool
	}{
		{
			name: "validation passes with lvm provisioner; no changes",
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						LVM: &diskv1.LVMProvisionerInfo{},
					},
					Provision: true,
				},
			},
			newBlockDeice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						LVM: &diskv1.LVMProvisionerInfo{},
					},
					Provision: true,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var bdCache ctldiskv1.BlockDeviceCache
			var scCache ctlstoragev1.StorageClassCache
			var pvCache ctlcorev1.PersistentVolumeCache
			var volCache lhv1beta2.VolumeCache
			var nodeCache ctlcorev1.NodeCache
			var vmImageCache ctlharvv1beta1.VirtualMachineImageCache
			if len(test.blockDeviceToCache) > 0 {
				bdCache = fake.NewBlockDeviceCache(test.blockDeviceToCache)
			}
			if len(test.scToCache) > 0 {
				scCache = fake.NewStorageClassCache(test.scToCache)
			}
			if len(test.pvToCache) > 0 {
				pvCache = fake.NewPersistentVolumeCache(test.pvToCache)
			}
			if len(test.volToCache) > 0 {
				volCache = fake.NewVolumeCache(test.volToCache)
			}
			if len(test.nodeToCache) > 0 {
				nodeCache = fake.NewNodeCache(test.nodeToCache)
			}
			if len(test.vmImageToCache) > 0 {
				vmImageCache = fake.NewVMImageCache(test.vmImageToCache)
			}
			validator := NewBlockdeviceValidator(bdCache, scCache, pvCache, volCache, nodeCache, vmImageCache)
			err := validator.Update(nil, test.newBlockDeice, test.oldBlockDevice)
			if test.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
