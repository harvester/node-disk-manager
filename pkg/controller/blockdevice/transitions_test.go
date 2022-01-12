package blockdevice

import (
	"testing"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/fake"
	"github.com/harvester/node-disk-manager/pkg/util"
	"github.com/harvester/node-disk-manager/pkg/util/fakeclients"
)

const (
	namespace = "test"
	nodeName  = "node0"
	bdName    = "test-bd"
	devPath   = "/dev/sda"
)

type infoImpl struct {
	mountPoint string
}

func (i *infoImpl) GetDisks() []*block.Disk                                  { return nil }
func (i *infoImpl) GetPartitions() []*block.Partition                        { return nil }
func (i *infoImpl) GetDiskByDevPath(name string) *block.Disk                 { return nil }
func (i *infoImpl) GetPartitionByDevPath(disk, part string) *block.Partition { return nil }
func (i *infoImpl) GetFileSystemInfoByDevPath(dname string) *block.FileSystemInfo {
	return &block.FileSystemInfo{
		Type:       "ext4",
		IsReadOnly: false,
		MountPoint: i.mountPoint,
	}
}

func Test_PhaseUnprovisioned(t *testing.T) {
	type input struct {
		bd   *diskv1.BlockDevice
		node *longhornv1.Node
	}
	type expected struct {
		phase diskv1.BlockDeviceProvisionPhase
		err   error
	}
	var testCases = []struct {
		name     string
		given    input
		expected expected
	}{
		{
			name: "unprovisioned",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName: nodeName,
						DevPath:  devPath,
						FileSystem: &diskv1.FilesystemInfo{
							ForceFormatted: true,
						},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnprovisioned,
				err:   nil,
			},
		},
		{
			name: "partition disk",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName: nodeName,
						DevPath:  devPath,
						FileSystem: &diskv1.FilesystemInfo{
							ForceFormatted: true,
						},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
						DeviceStatus: diskv1.DeviceStatus{
							Details: diskv1.DeviceDetails{
								DeviceType: diskv1.DeviceTypeDisk,
							},
						},
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhasePartitioning,
				err:   nil,
			},
		},
		{
			name: "already partitioned disk during resource lifecycle",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName: nodeName,
						DevPath:  devPath,
						FileSystem: &diskv1.FilesystemInfo{
							ForceFormatted: true,
						},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
						DeviceStatus: diskv1.DeviceStatus{
							Details: diskv1.DeviceDetails{
								DeviceType: diskv1.DeviceTypeDisk,
							},
						},
						Conditions: []diskv1.Condition{
							{
								Type:   diskv1.DevicePartitioned,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnprovisioned,
				err:   nil,
			},
		},
		{
			name: "already partitioned before resource initialization",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName:   nodeName,
						DevPath:    devPath,
						FileSystem: &diskv1.FilesystemInfo{},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
						DeviceStatus: diskv1.DeviceStatus{
							Partitioned: true,
							Details: diskv1.DeviceDetails{
								DeviceType: diskv1.DeviceTypeDisk,
							},
						},
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhasePartitioned,
				err:   nil,
			},
		},
		{
			name: "format partition",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName: nodeName,
						DevPath:  devPath,
						FileSystem: &diskv1.FilesystemInfo{
							ForceFormatted: true,
						},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
						DeviceStatus: diskv1.DeviceStatus{
							Details: diskv1.DeviceDetails{
								DeviceType: diskv1.DeviceTypePart,
							},
						},
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatting,
				err:   nil,
			},
		},
		{
			name: "already partitioned disk during resource lifecycle",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName: nodeName,
						DevPath:  devPath,
						FileSystem: &diskv1.FilesystemInfo{
							ForceFormatted: true,
						},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
						DeviceStatus: diskv1.DeviceStatus{
							Details: diskv1.DeviceDetails{
								DeviceType: diskv1.DeviceTypePart,
								UUID:       "727cac18-044b-4504-87f1-a5aefa774bda",
							},
						},
						Conditions: []diskv1.Condition{
							{
								Type:   diskv1.DeviceFormatted,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
				err:   nil,
			},
		}, {
			name: "already formatted before resource initialization",
			given: input{
				bd: &diskv1.BlockDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bdName,
						Namespace: namespace,
					},
					Spec: diskv1.BlockDeviceSpec{
						NodeName:   nodeName,
						DevPath:    devPath,
						FileSystem: &diskv1.FilesystemInfo{},
					},
					Status: diskv1.BlockDeviceStatus{
						ProvisionPhase: diskv1.ProvisionPhaseUnprovisioned,
						DeviceStatus: diskv1.DeviceStatus{
							Details: diskv1.DeviceDetails{
								DeviceType: diskv1.DeviceTypePart,
								UUID:       "727cac18-044b-4504-87f1-a5aefa774bda",
							},
						},
					},
				},
				node: &longhornv1.Node{},
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
				err:   nil,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clientset = fake.NewSimpleClientset()
			if tc.given.bd != nil {
				err := clientset.Tracker().Add(tc.given.bd)
				assert.Nil(t, err, "Mock resource should add into fake controller tracker")
			}
			if tc.given.node != nil {
				err := clientset.Tracker().Add(tc.given.node)
				assert.Nil(t, err, "Mock resource should add into fake controller tracker")
			}
			table := newTransitionTable(
				namespace,
				nodeName,
				fakeclients.BlockDeviceCache(clientset.HarvesterhciV1beta1().BlockDevices),
				fakeclients.NodeClient(clientset.LonghornV1beta1().Nodes),
				fakeclients.NodeCache(clientset.LonghornV1beta1().Nodes),
				nil,
			)

			// TODO: assertion on effect?
			phase, _, err := table.next(tc.given.bd)
			assert.Equal(t, tc.expected.err, err)
			assert.Equal(t, tc.expected.phase, phase)
		})
	}
}

func Test_PhaseVerbings(t *testing.T) {
	var testCases = []diskv1.BlockDeviceProvisionPhase{
		diskv1.ProvisionPhasePartitioning,
		diskv1.ProvisionPhaseFormatting,
		diskv1.ProvisionPhaseMounting,
		diskv1.ProvisionPhaseUnmounting,
		diskv1.ProvisionPhaseProvisioning,
		diskv1.ProvisionPhaseUnprovisioning,
	}

	for _, tc := range testCases {
		t.Run(string(tc), func(t *testing.T) {
			table := newTransitionTable(
				namespace,
				nodeName,
				nil,
				nil,
				nil,
				nil,
			)
			bd := &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bdName,
					Namespace: namespace,
				},
				Spec: diskv1.BlockDeviceSpec{
					NodeName:   nodeName,
					DevPath:    devPath,
					FileSystem: &diskv1.FilesystemInfo{},
				},
				Status: diskv1.BlockDeviceStatus{
					ProvisionPhase: tc,
				},
			}
			// TODO: assertion on effect?
			phase, _, err := table.next(bd)
			assert.Equal(t, tc, phase)
			assert.Nil(t, err)
		})
	}
}

func Test_PhaseFormatted(t *testing.T) {
	type input struct {
		mountPoint  string
		provisioned bool
	}
	type expected struct {
		phase diskv1.BlockDeviceProvisionPhase
	}
	var testCases = []struct {
		name     string
		given    input
		expected expected
	}{
		{
			name: "no mountpoints and no provision",
			given: input{
				mountPoint:  "",
				provisioned: false,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
			},
		},
		{
			name: "no mountpoints but need provision",
			given: input{
				mountPoint:  "",
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseMounting,
			},
		},
		{
			name: "same mountpoints but no provision",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: false,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
			},
		},
		{
			name: "same mountpoints and need provision",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseMounted,
			},
		},
		{
			name: "mountpoints differ and no provision",
			given: input{
				mountPoint:  "/home/deadbeef/feedcafe",
				provisioned: false,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
			},
		},
		{
			name: "mountpoints differ but need provision",
			given: input{
				mountPoint:  "/home/deadbeef/feedcafe",
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnmounting,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clientset = fake.NewSimpleClientset()
			bd := &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bdName,
					Namespace: namespace,
				},
				Spec: diskv1.BlockDeviceSpec{
					NodeName: nodeName,
					DevPath:  devPath,
					FileSystem: &diskv1.FilesystemInfo{
						Provisioned: tc.given.provisioned,
					},
				},
				Status: diskv1.BlockDeviceStatus{
					ProvisionPhase: diskv1.ProvisionPhaseFormatted,
				},
			}
			err := clientset.Tracker().Add(bd)
			assert.Nil(t, err, "Mock resource should add into fake controller tracker")

			table := newTransitionTable(
				namespace,
				nodeName,
				fakeclients.BlockDeviceCache(clientset.HarvesterhciV1beta1().BlockDevices),
				fakeclients.NodeClient(clientset.LonghornV1beta1().Nodes),
				fakeclients.NodeCache(clientset.LonghornV1beta1().Nodes),
				&infoImpl{mountPoint: tc.given.mountPoint},
			)

			// TODO: assertion on effect?
			phase, _, err := table.next(bd)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected.phase, phase)
		})
	}
}

func Test_PhaseMounted(t *testing.T) {
	type input struct {
		mountPoint   string
		provisioned  bool
		nodeNotFound bool
	}
	type expected struct {
		phase diskv1.BlockDeviceProvisionPhase
	}
	var testCases = []struct {
		name     string
		given    input
		expected expected
	}{
		{
			name: "no mountpoint and no provision",
			given: input{
				mountPoint:  "",
				provisioned: false,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
			},
		},
		{
			name: "no mountpoint but need provision",
			given: input{
				mountPoint:  "",
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
			},
		},
		{
			name: "with mountpoint but no provision",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: false,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnmounting,
			},
		},
		{
			name: "with mountpoint and need provision",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseProvisioning,
			},
		},
		{
			name: "with unexpected mountpoint and no provision",
			given: input{
				mountPoint:  "/home/deadbeef/feedcafe",
				provisioned: false,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnmounting,
			},
		},
		{
			name: "with unexpected mountpoint and need provision",
			given: input{
				mountPoint:  "/home/deadbeef/feedcafe",
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnmounting,
			},
		},
		{
			name: "with mountpoint and need provision but node not found",
			given: input{
				mountPoint:   util.GetMountPoint(bdName),
				provisioned:  true,
				nodeNotFound: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseMounted,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clientset = fake.NewSimpleClientset()
			bd := &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bdName,
					Namespace: namespace,
				},
				Spec: diskv1.BlockDeviceSpec{
					NodeName: nodeName,
					DevPath:  devPath,
					FileSystem: &diskv1.FilesystemInfo{
						Provisioned: tc.given.provisioned,
					},
				},
				Status: diskv1.BlockDeviceStatus{
					ProvisionPhase: diskv1.ProvisionPhaseMounted,
				},
			}
			err := clientset.Tracker().Add(bd)
			assert.Nil(t, err, "Mock resource should add into fake controller tracker")
			if !tc.given.nodeNotFound {
				node := &longhornv1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      nodeName,
						Namespace: namespace,
					},
					Spec:   lhtypes.NodeSpec{},
					Status: lhtypes.NodeStatus{},
				}
				err = clientset.Tracker().Add(node)
				assert.Nil(t, err, "Mock resource should add into fake controller tracker")
			}

			table := newTransitionTable(
				namespace,
				nodeName,
				fakeclients.BlockDeviceCache(clientset.HarvesterhciV1beta1().BlockDevices),
				fakeclients.NodeClient(clientset.LonghornV1beta1().Nodes),
				fakeclients.NodeCache(clientset.LonghornV1beta1().Nodes),
				&infoImpl{mountPoint: tc.given.mountPoint},
			)

			// TODO: assertion on effect?
			phase, _, err := table.next(bd)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected.phase, phase)
		})
	}
}

func Test_PhaseProvisioned(t *testing.T) {
	type input struct {
		mountPoint  string
		diskPath    string
		provisioned bool
	}
	type expected struct {
		phase diskv1.BlockDeviceProvisionPhase
	}
	var testCases = []struct {
		name     string
		given    input
		expected expected
	}{
		{
			name: "no mountpoint and no provision",
			given: input{
				mountPoint:  "",
				provisioned: false,
				diskPath:    "dummy",
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnprovisioning,
			},
		},
		{
			name: "no mountpoint but need provision",
			given: input{
				mountPoint:  "",
				provisioned: true,
				diskPath:    "dummy",
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseFormatted,
			},
		},
		{
			name: "with mountpoint but no provision",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: false,
				diskPath:    "dummy",
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnprovisioning,
			},
		},
		{
			name: "with mountpoint and need provision but no matched disk",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: true,
				diskPath:    "dummy",
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseUnprovisioning,
			},
		},
		{
			name: "with mountpoint and need provision and the same disk paths",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: true,
				diskPath:    util.GetMountPoint(bdName),
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseProvisioned,
			},
		},
		{
			name: "with mountpoint and need provision but no node",
			given: input{
				mountPoint:  util.GetMountPoint(bdName),
				provisioned: true,
			},
			expected: expected{
				phase: diskv1.ProvisionPhaseProvisioned,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var clientset = fake.NewSimpleClientset()
			bd := &diskv1.BlockDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      bdName,
					Namespace: namespace,
				},
				Spec: diskv1.BlockDeviceSpec{
					NodeName: nodeName,
					DevPath:  devPath,
					FileSystem: &diskv1.FilesystemInfo{
						Provisioned: tc.given.provisioned,
					},
				},
				Status: diskv1.BlockDeviceStatus{
					ProvisionPhase: diskv1.ProvisionPhaseProvisioned,
				},
			}
			err := clientset.Tracker().Add(bd)
			assert.Nil(t, err, "Mock resource should add into fake controller tracker")
			if tc.given.diskPath != "" {
				node := &longhornv1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:      nodeName,
						Namespace: namespace,
					},
					Spec: lhtypes.NodeSpec{
						Disks: map[string]lhtypes.DiskSpec{
							bdName: {
								Path: tc.given.diskPath,
							},
						},
					},
					Status: lhtypes.NodeStatus{},
				}
				err = clientset.Tracker().Add(node)
				assert.Nil(t, err, "Mock resource should add into fake controller tracker")
			}

			table := newTransitionTable(
				namespace,
				nodeName,
				fakeclients.BlockDeviceCache(clientset.HarvesterhciV1beta1().BlockDevices),
				fakeclients.NodeClient(clientset.LonghornV1beta1().Nodes),
				fakeclients.NodeCache(clientset.LonghornV1beta1().Nodes),
				&infoImpl{mountPoint: tc.given.mountPoint},
			)

			// TODO: assertion on effect?
			phase, _, err := table.next(bd)
			assert.Nil(t, err)
			assert.Equal(t, tc.expected.phase, phase)
		})
	}
}
