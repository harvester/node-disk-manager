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
		replicasToCache    []*lhv1.Replica
		oldBlockDevice     *diskv1.BlockDevice
		newBlockDeice      *diskv1.BlockDevice
		expectedErr        bool
	}{
		{
			name: "disk removal with a volume and backingimage that has healthy replicas elsewhere",
			replicasToCache: []*lhv1.Replica{
				newReplica("rep-1", "vol-1", "node-1", "disk-uuid-1", false),
				newReplica("rep-2", "vol-1", "node-2", "disk-uuid-2", false),
			},
			volsToCache: []*lhv1.Volume{
				newVolume("vol-1"),
			},
			lhNodesToCache: []*lhv1.Node{
				newLHNode("node-1", map[string]string{"blockdevice-to-remove": "disk-uuid-1"}),
			},
			biToCache: []*lhv1.BackingImage{
				newBackingImage("safe-image", map[string]lhv1.BackingImageState{
					"disk-uuid-1": lhv1.BackingImageStateReady,
					"disk-uuid-2": lhv1.BackingImageStateReady,
				}),
			},
			oldBlockDevice: newBlockDevice("blockdevice-to-remove", "node-1", true),
			newBlockDeice:  newBlockDevice("blockdevice-to-remove", "node-1", false),
			expectedErr:    false,
		},
		{
			name: "disk removal rejected with a volume with single replica and backing image with multiple replicas",
			replicasToCache: []*lhv1.Replica{
				newReplica("rep-1", "vol-1", "node-1", "disk-uuid-1", false),
			},
			volsToCache: []*lhv1.Volume{
				newVolume("vol-1"),
			},
			lhNodesToCache: []*lhv1.Node{
				newLHNode("node-1", map[string]string{"blockdevice-to-remove": "disk-uuid-1"}),
			},
			biToCache: []*lhv1.BackingImage{
				newBackingImage("safe-image", map[string]lhv1.BackingImageState{
					"disk-uuid-1": lhv1.BackingImageStateReady,
					"disk-uuid-2": lhv1.BackingImageStateReady,
				}),
			},
			oldBlockDevice: newBlockDevice("blockdevice-to-remove", "node-1", true),
			newBlockDeice:  newBlockDevice("blockdevice-to-remove", "node-1", false),
			expectedErr:    true,
		},
		{
			name: "disk removal rejected with replicated volume but single healthy backing image",
			replicasToCache: []*lhv1.Replica{
				newReplica("rep-1", "vol-1", "node-1", "disk-uuid-1", false),
				newReplica("rep-2", "vol-1", "node-2", "disk-uuid-2", false),
			},
			volsToCache: []*lhv1.Volume{
				newVolume("vol-1"),
			},
			lhNodesToCache: []*lhv1.Node{
				newLHNode("node-1", map[string]string{"blockdevice-to-remove": "disk-uuid-1"}),
			},
			biToCache: []*lhv1.BackingImage{
				newBackingImage("safe-image", map[string]lhv1.BackingImageState{
					"disk-uuid-1": lhv1.BackingImageStateReady,
				}),
			},
			oldBlockDevice: newBlockDevice("blockdevice-to-remove", "node-1", true),
			newBlockDeice:  newBlockDevice("blockdevice-to-remove", "node-1", false),
			expectedErr:    true,
		},
		{
			name: "disk removal allowed when volumes contain all failed replicas and replicated backing image",
			replicasToCache: []*lhv1.Replica{
				newReplica("rep-1", "vol-1", "node-1", "disk-uuid-1", true),
				newReplica("rep-2", "vol-1", "node-2", "disk-uuid-2", true),
			},
			volsToCache: []*lhv1.Volume{
				newVolume("vol-1"),
			},
			lhNodesToCache: []*lhv1.Node{
				newLHNode("node-1", map[string]string{"blockdevice-to-remove": "disk-uuid-1"}),
			},
			biToCache: []*lhv1.BackingImage{
				newBackingImage("safe-image", map[string]lhv1.BackingImageState{
					"disk-uuid-1": lhv1.BackingImageStateReady,
					"disk-uuid-2": lhv1.BackingImageStateReady,
				}),
			},
			oldBlockDevice: newBlockDevice("blockdevice-to-remove", "node-1", true),
			newBlockDeice:  newBlockDevice("blockdevice-to-remove", "node-1", false),
			expectedErr:    false,
		},
		{
			name: "disk removal allowed when volumes are all failed and backing images are all in non ready state",
			replicasToCache: []*lhv1.Replica{
				newReplica("rep-1", "vol-1", "node-1", "disk-uuid-1", true),
				newReplica("rep-2", "vol-1", "node-2", "disk-uuid-2", true),
			},
			volsToCache: []*lhv1.Volume{
				newVolume("vol-1"),
			},
			lhNodesToCache: []*lhv1.Node{
				newLHNode("node-1", map[string]string{"blockdevice-to-remove": "disk-uuid-1"}),
			},
			biToCache: []*lhv1.BackingImage{
				newBackingImage("safe-image", map[string]lhv1.BackingImageState{
					"disk-uuid-1": lhv1.BackingImageStateFailed,
					"disk-uuid-2": lhv1.BackingImageStateInProgress,
				}),
			},
			oldBlockDevice: newBlockDevice("blockdevice-to-remove", "node-1", true),
			newBlockDeice:  newBlockDevice("blockdevice-to-remove", "node-1", false),
			expectedErr:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var bdCache ctldiskv1.BlockDeviceCache
			var scCache ctlstoragev1.StorageClassCache
			var pvCache ctlcorev1.PersistentVolumeCache
			var volCache lhv1beta2.VolumeCache
			var lhNodeCache lhv1beta2.NodeCache
			var backingImageCache lhv1beta2.BackingImageCache
			var replicaCache lhv1beta2.ReplicaCache
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
			if test.lhNodesToCache != nil {
				lhNodeCache = fake.NewLonghornNodeCache(test.lhNodesToCache)
			}
			if test.biToCache != nil {
				backingImageCache = fake.NewBackingImageCache(test.biToCache)
			}
			if test.replicasToCache != nil {
				replicaCache = fake.NewReplicaCache(test.replicasToCache)
			}
			validator := NewBlockdeviceValidator(bdCache, scCache, pvCache, volCache, backingImageCache, lhNodeCache, replicaCache)
			err := validator.Update(nil, test.oldBlockDevice, test.newBlockDeice)
			if test.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func newBlockDevice(name, nodeName string, provision bool) *diskv1.BlockDevice {
	return &diskv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: diskv1.BlockDeviceSpec{
			Provisioner: &diskv1.ProvisionerInfo{
				Longhorn: &diskv1.LonghornProvisionerInfo{},
			},
			Provision: provision,
			NodeName:  nodeName,
		},
	}
}

func newLHNode(name string, disks map[string]string) *lhv1.Node {
	diskStatus := make(map[string]*lhv1.DiskStatus)
	for bdName, uuid := range disks {
		diskStatus[bdName] = &lhv1.DiskStatus{DiskUUID: uuid}
	}

	return &lhv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.LonghornSystemNamespaceName,
		},
		Status: lhv1.NodeStatus{
			DiskStatus: diskStatus,
		},
	}
}

func newVolume(name string) *lhv1.Volume {
	return &lhv1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.LonghornSystemNamespaceName,
		},
	}
}

func newReplica(name, volName, nodeID, diskID string, isFailed bool) *lhv1.Replica {
	failedAt := ""
	if isFailed {
		failedAt = "2025-10-13T10:00:00Z"
	}
	return &lhv1.Replica{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.LonghornSystemNamespaceName,
		},
		Spec: lhv1.ReplicaSpec{
			InstanceSpec: lhv1.InstanceSpec{
				VolumeName: volName,
				NodeID:     nodeID,
			},
			DiskID:   diskID,
			FailedAt: failedAt,
		},
	}
}

func newBackingImage(name string, diskStatuses map[string]lhv1.BackingImageState) *lhv1.BackingImage {
	statusMap := make(map[string]*lhv1.BackingImageDiskFileStatus)
	for uuid, state := range diskStatuses {
		statusMap[uuid] = &lhv1.BackingImageDiskFileStatus{State: state}
	}

	return &lhv1.BackingImage{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.LonghornSystemNamespaceName,
		},
		Status: lhv1.BackingImageStatus{
			DiskFileStatusMap: statusMap,
		},
	}
}
