package blockdevice

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/fake"
	"github.com/harvester/node-disk-manager/pkg/util/fakeclients"
	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	nodeName   = "test-node"
	namespace  = "test-namespace"
	deviceName = "test-blockdevice-name"
	devPath    = "/dev/testdevpath"
	mountPoint = "/mnt/mymountpoint"
)

func Test_addDeivceToNode(t *testing.T) {
	type input struct {
		conds  []diskv1.Condition
		disks  map[string]lhtypes.DiskSpec
		fsInfo *block.FileSystemInfo
	}
	type output struct {
		conds []diskv1.Condition
		disks map[string]lhtypes.DiskSpec
	}

	bd := diskv1.BlockDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name: deviceName,
		},
		Spec: diskv1.BlockDeviceSpec{
			DevPath: devPath,
			FileSystem: &diskv1.FilesystemInfo{
				MountPoint: mountPoint,
			},
		},
	}

	node := longhornv1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName,
			Namespace: namespace,
		},
		Spec: lhtypes.NodeSpec{
			Disks: map[string]lhtypes.DiskSpec{},
		},
	}

	var testCases = []struct {
		name     string
		given    input
		expected output
	}{
		{
			name: "empty filesystem",
			given: input{
				conds:  []diskv1.Condition{},
				disks:  map[string]lhtypes.DiskSpec{},
				fsInfo: nil,
			},
			expected: output{
				conds: []diskv1.Condition{},
				disks: map[string]lhtypes.DiskSpec{},
			},
		},
		{
			name: "empty mount point",
			given: input{
				conds: []diskv1.Condition{},
				disks: map[string]lhtypes.DiskSpec{},
				fsInfo: &block.FileSystemInfo{
					MountPoint: "",
				},
			},
			expected: output{
				conds: []diskv1.Condition{},
				disks: map[string]lhtypes.DiskSpec{},
			},
		},
		{
			name: "alreay got a disk with given mount point",
			given: input{
				conds: []diskv1.Condition{},
				disks: map[string]lhtypes.DiskSpec{
					deviceName: {
						Path: mountPoint,
						Tags: []string{"unique-token"},
					},
				},
				fsInfo: &block.FileSystemInfo{
					MountPoint: mountPoint,
				},
			},
			expected: output{
				conds: []diskv1.Condition{},
				disks: map[string]lhtypes.DiskSpec{
					deviceName: {
						Path: mountPoint,
						Tags: []string{"unique-token"},
					},
				},
			},
		},
		{
			name: "valid mount point",
			given: input{
				conds: []diskv1.Condition{},
				disks: map[string]lhtypes.DiskSpec{},
				fsInfo: &block.FileSystemInfo{
					MountPoint: mountPoint,
				},
			},
			expected: output{
				conds: []diskv1.Condition{
					{
						Type:    diskv1.DiskAddedToNode,
						Status:  "True",
						Message: fmt.Sprintf("Added disk %s to longhorn node `%s` as an additional disk", deviceName, nodeName),
					},
				},
				disks: map[string]lhtypes.DiskSpec{
					deviceName: {
						Path:              mountPoint,
						AllowScheduling:   true,
						EvictionRequested: false,
						StorageReserved:   0,
						Tags:              []string{},
					},
				},
			},
		},
		{
			name: "replace previous mounted disk",
			given: input{
				conds: []diskv1.Condition{
					{
						Type:    diskv1.DiskAddedToNode,
						Status:  "True",
						Message: "Previous mounted yay!",
					},
				},
				disks: map[string]lhtypes.DiskSpec{
					deviceName: {
						Path: "/dev/oldtestpath",
					},
				},
				fsInfo: &block.FileSystemInfo{
					MountPoint: mountPoint,
				},
			},
			expected: output{
				conds: []diskv1.Condition{
					{
						Type:    diskv1.DiskAddedToNode,
						Status:  "True",
						Message: fmt.Sprintf("Added disk %s to longhorn node `%s` as an additional disk", deviceName, nodeName),
					},
				},
				disks: map[string]lhtypes.DiskSpec{
					deviceName: {
						Path:              mountPoint,
						AllowScheduling:   true,
						EvictionRequested: false,
						StorageReserved:   0,
						Tags:              []string{},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			var clientset = fake.NewSimpleClientset()
			node := node.DeepCopy()
			node.Spec.Disks = tc.given.disks
			err := clientset.Tracker().Add(node)
			assert.Nil(t, err, "mock resource should add into fake controller tracker")
			bd := bd.DeepCopy()
			bd.Status.Conditions = tc.given.conds
			devPath := bd.Spec.DevPath
			fsInfo := tc.given.fsInfo
			info := &fakeInfo{}
			info.On("GetFileSystemInfoByDevPath", devPath).Return(fsInfo)
			ctrl := &Controller{
				Namespace: namespace,
				NodeName:  nodeName,
				NodeCache: fakeclients.NodeCache(clientset.LonghornV1beta1().Nodes),
				Nodes:     fakeclients.NodeClient(clientset.LonghornV1beta1().Nodes),
				BlockInfo: info,
			}

			// Act
			outputBd, err := ctrl.addDeviceToNode(bd)

			// Assert
			var actual output
			actual.conds = outputBd.Status.Conditions
			assert.Nil(t, err, "addDeviceToNode should return no error")
			assert.Equal(t, len(tc.expected.conds), len(actual.conds), "case %q", tc.name)
			if len(tc.expected.conds) > 0 {
				exp := tc.expected.conds[0]
				got := actual.conds[0]
				assert.Equal(t, exp.Message, got.Message, "case %q", tc.name)
				assert.Equal(t, exp.Type, got.Type, "case %q", tc.name)
				assert.Equal(t, exp.Status, got.Status, "case %q", tc.name)
			}
			outputNode, err := ctrl.NodeCache.Get(namespace, nodeName)
			actual.disks = outputNode.Spec.Disks
			assert.Nil(t, err, "Get should return no error")
			assert.Equal(t, tc.expected.disks, actual.disks, "case %q", tc.name)
		})
	}
}

type fakeInfo struct {
	mock.Mock
}

func (i *fakeInfo) GetDisks() []*block.Disk {
	panic("implement me")
}
func (i *fakeInfo) GetPartitions() []*block.Partition {
	panic("implement me")
}
func (i *fakeInfo) GetDiskByDevPath(name string) *block.Disk {
	panic("implement me")
}
func (i *fakeInfo) GetPartitionByDevPath(disk, part string) *block.Partition {
	panic("implement me")
}
func (i *fakeInfo) GetFileSystemInfoByDevPath(dname string) *block.FileSystemInfo {
	args := i.Called(dname)
	return args.Get(0).(*block.FileSystemInfo)
}
