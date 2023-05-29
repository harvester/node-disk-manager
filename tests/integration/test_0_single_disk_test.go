package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/melbahja/goph"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	clientset "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned"
)

type SingleDiskSuite struct {
	suite.Suite
	SSHClient      *goph.Client
	clientSet      *clientset.Clientset
	targetNodeName string
	targetDiskName string
}

type ProvisionedDisk struct {
	devPath string
	UUID    string
}

func (s *SingleDiskSuite) SetupSuite() {
	nodeName := ""
	f, _ := os.Open(filepath.Join(os.Getenv("NDM_HOME"), "ssh-config"))
	cfg, _ := ssh_config.Decode(f)
	// consider wildcard, so length shoule be 2
	require.Equal(s.T(), len(cfg.Hosts), 2, "number of Hosts on SSH-config should be 1")
	for _, host := range cfg.Hosts {
		if host.String() == "" {
			// wildcard, continue
			continue
		}
		nodeName = host.Patterns[0].String()
		break
	}
	require.NotEqual(s.T(), nodeName, "", "nodeName should not be empty.")
	s.targetNodeName = nodeName
	targetHost, _ := cfg.Get(nodeName, "HostName")
	targetUser, _ := cfg.Get(nodeName, "User")
	targetPrivateKey, _ := cfg.Get(nodeName, "IdentityFile")
	splitedResult := strings.Split(targetPrivateKey, "node-disk-manager/")
	privateKey := filepath.Join(os.Getenv("NDM_HOME"), splitedResult[len(splitedResult)-1])
	// Start new ssh connection with private key.
	auth, err := goph.Key(privateKey, "")
	require.Equal(s.T(), err, nil, "generate ssh auth key should not get error")

	s.SSHClient, err = goph.NewUnknown(targetUser, targetHost, auth)
	require.Equal(s.T(), err, nil, "New ssh connection should not get error")

	kubeconfig := filepath.Join(os.Getenv("NDM_HOME"), "kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.Equal(s.T(), err, nil, "Generate kubeconfig should not get error")

	s.clientSet, err = clientset.NewForConfig(config)
	require.Equal(s.T(), err, nil, "New clientset should not get error")
}

func (s *SingleDiskSuite) AfterTest(_, _ string) {
	if s.SSHClient != nil {
		s.SSHClient.Close()
	}
}

func TestSingleDiskOperation(t *testing.T) {
	suite.Run(t, new(SingleDiskSuite))
}

func (s *SingleDiskSuite) Test_0_AutoProvisionSingleDisk() {
	// prepare to check the added disk
	var provisionedDisk ProvisionedDisk
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	bdList, err := bdi.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), err, nil, "Get BlockdevicesList should not get error")
	require.NotEqual(s.T(), len(bdList.Items), 0, "BlockdevicesList should not be empty")
	for _, blockdevice := range bdList.Items {
		if blockdevice.Spec.NodeName != s.targetNodeName {
			// focus the target node
			continue
		}
		bdStatus := blockdevice.Status
		if bdStatus.State == "Active" && bdStatus.ProvisionPhase == "Provisioned" {
			s.targetDiskName = blockdevice.Name
			// get from blockdevice resource
			provisionedDisk.devPath = bdStatus.DeviceStatus.DevPath
			provisionedDisk.UUID = bdStatus.DeviceStatus.Details.UUID

			// checking with the device on the host
			cmd := "sudo blkid -s UUID name -o value " + provisionedDisk.devPath
			out, err := s.SSHClient.Run(cmd)
			require.Equal(s.T(), err, nil, "Running command `blkid` should not get error")
			require.NotEqual(s.T(), "", string(out), "blkid command should not return empty, ", provisionedDisk.devPath)
			convertOutPut := strings.Split(string(out), "\n")[0]
			require.Equal(s.T(), provisionedDisk.UUID, convertOutPut, "Provisioned disk UUID should be the same")
		}
	}
}

func (s *SingleDiskSuite) Test_1_UnprovisionSingleDisk() {
	require.NotEqual(s.T(), s.targetDiskName, "", "target disk name should not be empty before we do the remove test")
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	curBlockdevice, err := bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get Blockdevices should not get error")

	newBlockdevice := curBlockdevice.DeepCopy()
	newBlockdevice.Spec.FileSystem.Provisioned = false
	bdi.Update(context.TODO(), newBlockdevice, v1.UpdateOptions{})

	// sleep 30 seconds to wait controller handle. jitter is between 7~13 seconds so 30 seconds would be enough to run twice
	time.Sleep(30 * time.Second)

	// check for the removed status
	curBlockdevice, err = bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get BlockdevicesList should not get error before we want to check remove")
	require.Equal(s.T(), curBlockdevice.Status.DeviceStatus.FileSystem.MountPoint, "", "Mountpoint should be empty after we remove disk!")
	require.Equal(s.T(), diskv1.ProvisionPhaseUnprovisioned, curBlockdevice.Status.ProvisionPhase, "Block device provisionPhase should be Provisioned")

}

func (s *SingleDiskSuite) Test_2_ManuallyProvisionSingleDisk() {
	require.NotEqual(s.T(), s.targetDiskName, "", "target disk name should not be empty before we do the remove test")
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	curBlockdevice, err := bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get Blockdevices should not get error")

	newBlockdevice := curBlockdevice.DeepCopy()
	newBlockdevice.Spec.FileSystem.Provisioned = true
	bdi.Update(context.TODO(), newBlockdevice, v1.UpdateOptions{})

	// sleep 3 seconds to wait controller handle
	time.Sleep(3 * time.Second)

	// check for the added status
	curBlockdevice, err = bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get BlockdevicesList should not get error before we want to check remove")
	require.NotEqual(s.T(), curBlockdevice.Status.DeviceStatus.FileSystem.MountPoint, "", "Mountpoint should be empty after we remove disk!")
	require.Equal(s.T(), diskv1.ProvisionPhaseProvisioned, curBlockdevice.Status.ProvisionPhase, "Block device provisionPhase should be Provisioned")
	require.Equal(s.T(), diskv1.BlockDeviceActive, curBlockdevice.Status.State, "Block device State should be Active")
}
