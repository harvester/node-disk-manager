package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	"github.com/harvester/node-disk-manager/pkg/utils"
)

/*
 * We have some assumption for the hotplug test:
 * 1. we will reuse the disk that is added on the initinal operation of ci test
 * 2. we use virsh command to remove disk/add back disk directly
 *
 * NOTE: The default qcow2 and xml location (created by initial operation) is `/tmp/hotplug_disks/`.
 *       File names are `node1-sda.qcow2` and `node1-sda.xml`.
 *       The target node name is `ndm-vagrant-k3s_node1`.
 */

const (
	hotplugDiskXMLFileName = "/tmp/hotplug_disks/node1-sda.xml"
	hotplugTargetDiskName  = "sda"
)

type HotPlugTestSuite struct {
	suite.Suite
	SSHClient             *goph.Client
	clientSet             *clientset.Clientset
	targetNodeName        string
	targetDiskName        string
	hotplugTargetNodeName string
	hotplugTargetBaseDir  string
}

func (s *HotPlugTestSuite) SetupSuite() {
	nodeName := ""
	f, err := os.Open(filepath.Join(os.Getenv("NDM_HOME"), "ssh-config"))
	require.Equal(s.T(), nil, err, "Open ssh-config should not get error")
	cfg, err := ssh_config.Decode(f)
	require.Equal(s.T(), nil, err, "Decode ssh-config should not get error")
	// consider wildcard, so length shoule be 2
	require.Equal(s.T(), 2, len(cfg.Hosts), "number of Hosts on SSH-config should be 1")
	for _, host := range cfg.Hosts {
		if host.String() == "" {
			// wildcard, continue
			continue
		}
		nodeName = host.Patterns[0].String()
		break
	}
	require.NotEqual(s.T(), "", nodeName, "nodeName should not be empty.")
	s.targetNodeName = nodeName
	targetHost, _ := cfg.Get(nodeName, "HostName")
	targetUser, _ := cfg.Get(nodeName, "User")
	targetPrivateKey, _ := cfg.Get(nodeName, "IdentityFile")
	splitedResult := strings.Split(targetPrivateKey, "node-disk-manager/")
	privateKey := filepath.Join(os.Getenv("NDM_HOME"), splitedResult[len(splitedResult)-1])
	// Start new ssh connection with private key.
	auth, err := goph.Key(privateKey, "")
	require.Equal(s.T(), nil, err, "generate ssh auth key should not get error")

	s.SSHClient, err = goph.NewUnknown(targetUser, targetHost, auth)
	require.Equal(s.T(), nil, err, "New ssh connection should not get error")

	kubeconfig := filepath.Join(os.Getenv("NDM_HOME"), "kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.Equal(s.T(), nil, err, "Generate kubeconfig should not get error")

	s.clientSet, err = clientset.NewForConfig(config)
	require.Equal(s.T(), nil, err, "New clientset should not get error")

	cmd := fmt.Sprintf("ls %s |grep vagrant-k3s", os.Getenv("NDM_HOME"))
	targetDirDomain, _, err := doCommand(cmd)
	require.Equal(s.T(), nil, err, "Running command `%s` should not get error : %v", cmd, err)

	s.hotplugTargetNodeName = fmt.Sprintf("%s_node1", strings.TrimSpace(targetDirDomain))
	s.hotplugTargetBaseDir = fmt.Sprintf("/tmp/hotplug_disks/%s", strings.TrimSpace(targetDirDomain))

}

func (s *HotPlugTestSuite) AfterTest(_, _ string) {
	if s.SSHClient != nil {
		s.SSHClient.Close()
	}
}

func (s *HotPlugTestSuite) SetupTest() {
	if skipNext {
		s.T().Skip("Skipping test because a previous test failed")
	}
}

func (s *HotPlugTestSuite) TearDownTest() {
	if s.T().Failed() {
		skipNext = true
	}
}

func TestHotPlugDisk(t *testing.T) {
	suite.Run(t, new(HotPlugTestSuite))
}

func (s *HotPlugTestSuite) Test_0_PreCheckForDiskCount() {
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	bdList, err := bdi.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), nil, err, "Get BlockdevicesList should not get error")
	diskCount := 0
	for _, blockdevice := range bdList.Items {
		if blockdevice.Spec.NodeName != s.targetNodeName {
			// focus the target node
			continue
		}
		bdStatus := blockdevice.Status
		if bdStatus.State == "Active" && bdStatus.ProvisionPhase == "Provisioned" {
			diskCount++
			s.targetDiskName = blockdevice.Name
		}
	}
	require.Equal(s.T(), 1, diskCount, "We should only have one disk.")
}

func (s *HotPlugTestSuite) Test_1_HotPlugRemoveDisk() {
	// remove disk dynamically
	cmd := fmt.Sprintf("virsh detach-disk %s %s --live", s.hotplugTargetNodeName, hotplugTargetDiskName)
	_, _, err := doCommand(cmd)
	require.Equal(s.T(), err, nil, "Running command `%s` should not get error", cmd)

	// wait for controller handling
	time.Sleep(5 * time.Second)

	// check disk status
	require.NotEqual(s.T(), "", s.targetDiskName, "target disk name should not be empty before we start hotplug (remove) test")
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	curBlockdevice, err := bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), nil, err, "Get Blockdevices should not get error")

	require.Equal(s.T(), diskv1.BlockDeviceInactive, curBlockdevice.Status.State, "Disk status should be inactive after we remove disk")

}

func (s *HotPlugTestSuite) Test_2_HotPlugAddDisk() {
	// remove disk dynamically
	hotplugDiskXMLFileName := fmt.Sprintf("%s/node1-sda.xml", s.hotplugTargetBaseDir)
	cmd := fmt.Sprintf("virsh attach-device --domain %s --file %s --live", s.hotplugTargetNodeName, hotplugDiskXMLFileName)
	_, _, err := doCommand(cmd)
	require.Equal(s.T(), nil, err, "Running command `%s` should not get error", cmd)

	// wait for controller handling, the device will be changed need more time to wait for the controller handling
	time.Sleep(30 * time.Second)

	// check disk status
	require.NotEqual(s.T(), s.targetDiskName, "", "target disk name should not be empty before we start hotplug (add) test")
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	curBlockdevice, err := bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), nil, err, "Get Blockdevices should not get error")

	require.Equal(s.T(), diskv1.BlockDeviceActive, curBlockdevice.Status.State, "Disk status should be inactive after we add disk")
}

func (s *HotPlugTestSuite) Test_3_AddDuplicatedWWNDsik() {
	// create another another disk raw file and xml

	originalDeviceRaw := fmt.Sprintf("%s/node1-sda.qcow2", s.hotplugTargetBaseDir)
	duplicatedDeviceXML := fmt.Sprintf("%s/node1-sdb.xml", s.hotplugTargetBaseDir)
	duplicatedDeviceRaw := fmt.Sprintf("%s/node1-sdb.qcow2", s.hotplugTargetBaseDir)

	cmdCpyRawFile := fmt.Sprintf("cp %s %s", originalDeviceRaw, duplicatedDeviceRaw)
	_, _, err := doCommand(cmdCpyRawFile)
	require.Equal(s.T(), nil, err, "Running command `%s` should not get error", cmdCpyRawFile)

	hotplugDiskXMLFileName := fmt.Sprintf("%s/node1-sda.xml", s.hotplugTargetBaseDir)
	disk, err := utils.DiskXMLReader(hotplugDiskXMLFileName)
	require.Equal(s.T(), nil, err, "Read xml file should not get error")
	disk.Source.File = duplicatedDeviceRaw
	disk.Target.Dev = "sdb"
	disk.VENDOR = "HARV"
	err = utils.XMLWriter(duplicatedDeviceXML, disk)
	require.Equal(s.T(), nil, err, "Write xml file should not get error")

	cmd := fmt.Sprintf("virsh attach-device --domain %s --file %s --live", s.hotplugTargetNodeName, duplicatedDeviceXML)
	_, _, err = doCommand(cmd)
	require.Equal(s.T(), nil, err, "Running command `%s` should not get error", cmd)

	// wait for controller handling
	time.Sleep(5 * time.Second)

	// check disk status
	require.NotEqual(s.T(), "", s.targetDiskName, "target disk name should not be empty before we start hotplug (add) test")
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	blockdeviceList, err := bdi.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), nil, err, "Get BlockdevicesList should not get error")
	require.Equal(s.T(), 1, len(blockdeviceList.Items), "We should have one disks because duplicated wwn should not added")

	// cleanup this disk
	cmd = fmt.Sprintf("virsh detach-disk %s %s --live", s.hotplugTargetNodeName, "sdb")
	_, _, err = doCommand(cmd)
	require.Equal(s.T(), nil, err, "Running command `%s` should not get error", cmd)

	// wait for controller handling
	time.Sleep(5 * time.Second)
}

func (s *HotPlugTestSuite) Test_4_RemoveInactiveDisk() {
	// remove disk dynamically
	cmd := fmt.Sprintf("virsh detach-disk %s %s --live", s.hotplugTargetNodeName, hotplugTargetDiskName)
	_, _, err := doCommand(cmd)
	require.Equal(s.T(), nil, err, "Running command `%s` should not get error", cmd)

	// wait for controller handling
	time.Sleep(5 * time.Second)

	// check disk status
	require.NotEqual(s.T(), s.targetDiskName, "", "target disk name should not be empty before we start hotplug (remove) test")
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	curBlockdevice, err := bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), nil, err, "Get Blockdevices should not get error")

	require.Equal(s.T(), diskv1.BlockDeviceInactive, curBlockdevice.Status.State, "Disk status should be inactive after we remove disk")

	// remove this inactive device from Harvester
	newBlockdevice := curBlockdevice.DeepCopy()
	newBlockdevice.Spec.FileSystem.Provisioned = false
	bdi.Update(context.TODO(), newBlockdevice, v1.UpdateOptions{})

	// sleep 30 seconds to wait controller handle. jitter is between 7~13 seconds so 30 seconds would be enough to run twice
	time.Sleep(30 * time.Second)

	// check for the removed status
	curBlockdevice, err = bdi.Get(context.TODO(), s.targetDiskName, v1.GetOptions{})
	require.Equal(s.T(), nil, err, "Get BlockdevicesList should not get error before we want to check remove")
	require.Equal(s.T(), "", curBlockdevice.Status.DeviceStatus.FileSystem.MountPoint, "Mountpoint should be empty after we remove disk!")
	require.Equal(s.T(), diskv1.ProvisionPhaseUnprovisioned, curBlockdevice.Status.ProvisionPhase, "Block device provisionPhase should be Unprovisioned")
}

func doCommand(cmdString string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command("bash", "-c", cmdString)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
