package blockdevice

import (
	"testing"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/fake"
	"github.com/harvester/node-disk-manager/pkg/util/fakeclients"
)

const (
	namespace = "test"
	nodeName  = "node0"
	bdName    = "test-bd"
	devPath   = "/dev/sda"
)

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
