package blockdevice

import (
	"testing"

	harvv1beta1 "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	ctlharvv1beta1 "github.com/harvester/harvester/pkg/generated/controllers/harvesterhci.io/v1beta1"
	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/utils"
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
		scsToCache         []*storagev1.StorageClass
		pvsToCache         []*v1.PersistentVolume
		volsToCache        []*lhv1.Volume
		nodesToCache       []*v1.Node
		vmImagesToCache    []*harvv1beta1.VirtualMachineImage
		oldBlockDevice     *diskv1.BlockDevice
		newBlockDeice      *diskv1.BlockDevice
		expectedErr        bool
	}{
		{
			name: "disk removal passes with empty volumes on single with successful vm images",
			nodesToCache: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
			},
			volsToCache: []*lhv1.Volume{},
			pvsToCache: []*v1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vol-1",
					},
					Spec: v1.PersistentVolumeSpec{
						PersistentVolumeSource: v1.PersistentVolumeSource{
							CSI: &v1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									utils.DiskSelectorKey: "disk1",
								},
							},
						},
					},
				},
			},
			vmImagesToCache: []*harvv1beta1.VirtualMachineImage{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ubuntu",
					},
					Spec: harvv1beta1.VirtualMachineImageSpec{
						StorageClassParameters: map[string]string{
							utils.DiskSelectorKey: "disk1",
						},
					},
					Status: harvv1beta1.VirtualMachineImageStatus{
						Failed: 0,
					},
				},
			},
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: true,
					Tags:      []string{"disk1"},
				},
			},
			newBlockDeice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: false,
					Tags:      []string{"disk1"},
				},
			},
			expectedErr: false,
		},
		{
			name: "disk removal passes with healthy volumes on single node no vm images",
			nodesToCache: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
			},
			volsToCache: []*lhv1.Volume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vol-1",
						Namespace: utils.LonghornSystemNamespaceName,
					},
					Status: lhv1.VolumeStatus{
						Robustness: lhv1.VolumeRobustnessHealthy,
					},
				},
			},
			pvsToCache: []*v1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vol-1",
					},
					Spec: v1.PersistentVolumeSpec{
						PersistentVolumeSource: v1.PersistentVolumeSource{
							CSI: &v1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									utils.DiskSelectorKey: "disk1",
								},
							},
						},
					},
				},
			},
			vmImagesToCache: []*harvv1beta1.VirtualMachineImage{},
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: true,
					Tags:      []string{"disk1"},
				},
			},
			newBlockDeice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: false,
					Tags:      []string{"disk1"},
				},
			},
			expectedErr: false,
		},
		{
			name: "disk removal rejected with degraded volume on single node with successful vm image",
			nodesToCache: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
			},
			volsToCache: []*lhv1.Volume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vol-1",
						Namespace: utils.LonghornSystemNamespaceName,
					},
					Status: lhv1.VolumeStatus{
						Robustness: lhv1.VolumeRobustnessDegraded,
					},
				},
			},
			pvsToCache: []*v1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vol-1",
					},
					Spec: v1.PersistentVolumeSpec{
						PersistentVolumeSource: v1.PersistentVolumeSource{
							CSI: &v1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									utils.DiskSelectorKey: "disk1",
								},
							},
						},
					},
				},
			},
			vmImagesToCache: []*harvv1beta1.VirtualMachineImage{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ubuntu",
					},
					Spec: harvv1beta1.VirtualMachineImageSpec{
						StorageClassParameters: map[string]string{
							utils.DiskSelectorKey: "disk1",
						},
					},
					Status: harvv1beta1.VirtualMachineImageStatus{
						Failed: 0,
					},
				},
			},
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: true,
					Tags:      []string{"disk1"},
				},
			},
			newBlockDeice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: false,
					Tags:      []string{"disk1"},
				},
			},
			expectedErr: true,
		},
		{
			name: "disk removal rejected with healthy volume on single node but with failed vm image",
			nodesToCache: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
			},
			volsToCache: []*lhv1.Volume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vol-1",
						Namespace: utils.LonghornSystemNamespaceName,
					},
					Status: lhv1.VolumeStatus{
						Robustness: lhv1.VolumeRobustnessHealthy,
					},
				},
			},
			pvsToCache: []*v1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vol-1",
					},
					Spec: v1.PersistentVolumeSpec{
						PersistentVolumeSource: v1.PersistentVolumeSource{
							CSI: &v1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									utils.DiskSelectorKey: "disk1",
								},
							},
						},
					},
				},
			},
			vmImagesToCache: []*harvv1beta1.VirtualMachineImage{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ubuntu",
					},
					Spec: harvv1beta1.VirtualMachineImageSpec{
						StorageClassParameters: map[string]string{
							utils.DiskSelectorKey: "disk1",
						},
					},
					Status: harvv1beta1.VirtualMachineImageStatus{
						Failed: 1,
					},
				},
			},
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: true,
					Tags:      []string{"disk1"},
				},
			},
			newBlockDeice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: false,
					Tags:      []string{"disk1"},
				},
			},
			expectedErr: true,
		},
		{
			name: "disk removal passes on multi node with healthy volume but with failed vm image",
			nodesToCache: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
			},
			volsToCache: []*lhv1.Volume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vol-1",
						Namespace: utils.LonghornSystemNamespaceName,
					},
					Status: lhv1.VolumeStatus{
						Robustness: lhv1.VolumeRobustnessHealthy,
					},
				},
			},
			pvsToCache: []*v1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vol-1",
					},
					Spec: v1.PersistentVolumeSpec{
						PersistentVolumeSource: v1.PersistentVolumeSource{
							CSI: &v1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									utils.DiskSelectorKey: "disk1",
								},
							},
						},
					},
				},
			},
			vmImagesToCache: []*harvv1beta1.VirtualMachineImage{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ubuntu",
					},
					Spec: harvv1beta1.VirtualMachineImageSpec{
						StorageClassParameters: map[string]string{
							utils.DiskSelectorKey: "disk1",
						},
					},
					Status: harvv1beta1.VirtualMachineImageStatus{
						Failed: 1,
					},
				},
			},
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: true,
					Tags:      []string{"disk1"},
				},
			},
			newBlockDeice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: false,
					Tags:      []string{"disk1"},
				},
			},
			expectedErr: false,
		},
		{
			name: "disk removal passes on default disk with no tags",
			nodesToCache: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "harvester",
					},
				},
			},
			volsToCache: []*lhv1.Volume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vol-1",
						Namespace: utils.LonghornSystemNamespaceName,
					},
					Status: lhv1.VolumeStatus{
						Robustness: lhv1.VolumeRobustnessHealthy,
					},
				},
			},
			pvsToCache: []*v1.PersistentVolume{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vol-1",
					},
					Spec: v1.PersistentVolumeSpec{
						PersistentVolumeSource: v1.PersistentVolumeSource{
							CSI: &v1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									utils.DiskSelectorKey: "disk1",
								},
							},
						},
					},
				},
			},
			vmImagesToCache: []*harvv1beta1.VirtualMachineImage{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ubuntu",
					},
					Spec: harvv1beta1.VirtualMachineImageSpec{
						StorageClassParameters: map[string]string{
							utils.DiskSelectorKey: "disk1",
						},
					},
					Status: harvv1beta1.VirtualMachineImageStatus{
						Failed: 1,
					},
				},
			},
			oldBlockDevice: &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "testbd",
				},
				Spec: diskv1.BlockDeviceSpec{
					Provisioner: &diskv1.ProvisionerInfo{
						Longhorn: &diskv1.LonghornProvisionerInfo{},
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
						Longhorn: &diskv1.LonghornProvisionerInfo{},
					},
					Provision: false,
				},
			},
			expectedErr: false,
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
			if test.blockDeviceToCache != nil {
				bdCache = fake.NewBlockDeviceCache(test.blockDeviceToCache)
			}
			if test.scsToCache != nil {
				scCache = fake.NewStorageClassCache(test.scsToCache)
			}
			if test.pvsToCache != nil {
				pvCache = fake.NewPersistentVolumeCache(test.pvsToCache)
			}
			if test.volsToCache != nil {
				volCache = fake.NewVolumeCache(test.volsToCache)
			}
			if test.nodesToCache != nil {
				nodeCache = fake.NewNodeCache(test.nodesToCache)
			}
			if test.vmImagesToCache != nil {
				vmImageCache = fake.NewVMImageCache(test.vmImagesToCache)
			}
			validator := NewBlockdeviceValidator(bdCache, scCache, pvCache, volCache, nodeCache, vmImageCache)
			err := validator.Update(nil, test.oldBlockDevice, test.newBlockDeice)
			if test.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
