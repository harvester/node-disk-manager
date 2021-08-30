package node

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/fake"
	"github.com/harvester/node-disk-manager/pkg/util/fakeclients"
	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"github.com/stretchr/testify/assert"
)

const (
	deviceNameA = "test-blockdevice-name-a"
	deviceNameB = "test-blockdevice-name-b"
	nodeNameA   = "test-node-a"
	nodeNameB   = "test-node-b"
	namespace   = "test-namespace"
)

func Test_OnNodeDelete(t *testing.T) {

	type input struct {
		node *longhornv1.Node
	}
	type output struct {
		deviceNames []string
	}

	bds := []diskv1.BlockDevice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deviceNameA,
				Namespace: namespace,
				Labels: map[string]string{
					v1.LabelHostname: nodeNameA,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deviceNameB,
				Namespace: namespace,
				Labels: map[string]string{
					v1.LabelHostname: nodeNameB,
				},
			},
		},
	}

	var testCases = []struct {
		name     string
		given    input
		expected output
	}{
		{
			name: "given a nil node",
			given: input{
				node: nil,
			},
			expected: output{
				deviceNames: []string{deviceNameA, deviceNameB},
			},
		},
		{
			name: "remove device under the node",
			given: input{
				node: &longhornv1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      nodeNameA,
						Namespace: namespace,
					},
				},
			},
			expected: output{
				deviceNames: []string{deviceNameB},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			var clientset = fake.NewSimpleClientset()
			if tc.given.node != nil {
				err := clientset.Tracker().Add(tc.given.node)
				assert.Nil(t, err, "mock resource should add into fake controller tracker")
			}
			for _, bd := range bds {
				bd := bd.DeepCopy()
				err := clientset.Tracker().Add(bd)
				assert.Nil(t, err, "mock resource should add into fake controller tracker")
			}
			ctrl := &Controller{
				namespace:        namespace,
				BlockDevices:     fakeclients.BlockeDeviceClient(clientset.HarvesterhciV1beta1().BlockDevices),
				BlockDeviceCache: fakeclients.BlockeDeviceCache(clientset.HarvesterhciV1beta1().BlockDevices),
			}

			// Act
			outputNode, err := ctrl.OnNodeDelete("fake-key", tc.given.node)

			// Assert
			assert.Nil(t, err, "OnNodeDelete should return no error")
			if tc.given.node == nil {
				assert.Nil(t, outputNode, err, "output node should be nil")
			} else {
				var actual output
				var err error
				actual.deviceNames = []string{}
				bds, err := ctrl.BlockDevices.List(namespace, metav1.ListOptions{})
				assert.Nil(t, err, "List should return no error")
				for _, bd := range bds.Items {
					actual.deviceNames = append(actual.deviceNames, bd.Name)
				}
				assert.ElementsMatch(t, actual.deviceNames, tc.expected.deviceNames, "case %q", tc.name)
			}
		})
	}
}
