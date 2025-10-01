package blockdevice

import (
	"testing"

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
		biToCache          []*lhv1.BackingImage
		lhNodesToCache     []*lhv1.Node
		oldBlockDevice     *diskv1.BlockDevice
		newBlockDeice      *diskv1.BlockDevice
		expectedErr        bool
	}{
		{
			name: "disk removal passes with empty volumes on single node with successful backing images",
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
			biToCache: []*lhv1.BackingImage{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ready-image"},
					Status: lhv1.BackingImageStatus{
						DiskFileStatusMap: map[string]*lhv1.BackingImageDiskFileStatus{
							"1234": {State: lhv1.BackingImageStateReady},
						},
					},
				},
			},
			lhNodesToCache: []*lhv1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "harvester"},
					Status: lhv1.NodeStatus{
						DiskStatus: map[string]*lhv1.DiskStatus{
							"testbd": {DiskUUID: "1234"},
						},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
				},
			},
			expectedErr: false,
		},
		{
			name: "disk removal passes with healthy volumes on single node no backing images",
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
			biToCache:      []*lhv1.BackingImage{},
			lhNodesToCache: []*lhv1.Node{},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
				},
			},
			expectedErr: false,
		},
		{
			name: "disk removal passes with empty volumes on single node with failed backing image not related to the disk",
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
			biToCache: []*lhv1.BackingImage{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "failed-image"},
					Status: lhv1.BackingImageStatus{
						DiskFileStatusMap: map[string]*lhv1.BackingImageDiskFileStatus{
							"1234": {State: lhv1.BackingImageStateReady},
						},
					},
				},
			},
			lhNodesToCache: []*lhv1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "harvester"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
				},
			},
			expectedErr: false,
		},
		{
			name: "disk removal rejected with degraded volume on single node with successful backing image",
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
			biToCache: []*lhv1.BackingImage{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "ready-image"},
					Status: lhv1.BackingImageStatus{
						DiskFileStatusMap: map[string]*lhv1.BackingImageDiskFileStatus{
							"1234": {State: lhv1.BackingImageStateReady},
						},
					},
				},
			},
			lhNodesToCache: []*lhv1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "harvester"},
					Status: lhv1.NodeStatus{
						DiskStatus: map[string]*lhv1.DiskStatus{
							"testbd": {DiskUUID: "1234"},
						},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
				},
			},
			expectedErr: true,
		},
		{
			name: "disk removal rejected with healthy volume on single node but with failed backing image",
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
			biToCache: []*lhv1.BackingImage{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "failed-image"},
					Status: lhv1.BackingImageStatus{
						DiskFileStatusMap: map[string]*lhv1.BackingImageDiskFileStatus{
							"1234": {State: lhv1.BackingImageStateFailed},
						},
					},
				},
			},
			lhNodesToCache: []*lhv1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "harvester"},
					Status: lhv1.NodeStatus{
						DiskStatus: map[string]*lhv1.DiskStatus{
							"testbd": {DiskUUID: "1234"},
						},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
				},
			},
			expectedErr: true,
		},
		{
			name: "disk removal passes on multi node with healthy volume but with failed backing image",
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
			biToCache: []*lhv1.BackingImage{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "failed-image"},
					Status: lhv1.BackingImageStatus{
						DiskFileStatusMap: map[string]*lhv1.BackingImageDiskFileStatus{
							"1234": {State: lhv1.BackingImageStateFailed},
						},
					},
				},
			},
			lhNodesToCache: []*lhv1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "harvester"},
					Status: lhv1.NodeStatus{
						DiskStatus: map[string]*lhv1.DiskStatus{
							"testbd": {DiskUUID: "1234"},
						},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
				Status: diskv1.BlockDeviceStatus{
					Tags: []string{"disk1"},
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
			biToCache:      []*lhv1.BackingImage{},
			lhNodesToCache: []*lhv1.Node{},
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
			var lhNodeCache lhv1beta2.NodeCache
			var backingImageCache lhv1beta2.BackingImageCache
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
			if test.lhNodesToCache != nil {
				lhNodeCache = fake.NewLonghornNodeCache(test.lhNodesToCache)
			}
			if test.biToCache != nil {
				backingImageCache = fake.NewBackingImageCache(test.biToCache)
			}
			validator := NewBlockdeviceValidator(bdCache, scCache, pvCache, volCache, nodeCache, backingImageCache, lhNodeCache)
			err := validator.Update(nil, test.oldBlockDevice, test.newBlockDeice)
			if test.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
