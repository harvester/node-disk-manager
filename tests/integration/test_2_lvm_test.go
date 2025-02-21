package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/melbahja/goph"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	clientset "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type LVMSuite struct {
	suite.Suite
	SSHClient             *goph.Client
	coreClientSet         *kubernetes.Clientset
	clientSet             *clientset.Clientset
	targetNodeName        string
	hotplugTargetNodeName string
	hotplugTargetBaseDir  string
	targetDevFirst        *diskv1.BlockDevice
	targetDevSecond       *diskv1.BlockDevice
}

func (s *LVMSuite) SetupSuite() {
	nodeName := ""
	f, err := os.Open(filepath.Join(os.Getenv("NDM_HOME"), "ssh-config"))
	require.Equal(s.T(), err, nil, "Open ssh-config should not get error")
	cfg, err := ssh_config.Decode(f)
	require.Equal(s.T(), err, nil, "Decode ssh-config should not get error")
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

	s.coreClientSet, err = kubernetes.NewForConfig(config)
	require.Equal(s.T(), err, nil, "New clientset(K8S) should not get error")

	s.clientSet, err = clientset.NewForConfig(config)
	require.Equal(s.T(), err, nil, "New clientset(NDM) should not get error")

	cmd := fmt.Sprintf("ls %s |grep vagrant-k3s", os.Getenv("NDM_HOME"))
	targetDirDomain, _, err := doCommand(cmd)
	require.Equal(s.T(), err, nil, "Running command `%s` should not get error : %v", cmd, err)

	s.hotplugTargetNodeName = fmt.Sprintf("%s_node1", strings.TrimSpace(targetDirDomain))
	s.hotplugTargetBaseDir = fmt.Sprintf("/tmp/hotplug_disks/%s", strings.TrimSpace(targetDirDomain))

	// cleanup the previous blockdevices
	s.cleanupBlockDevices()

	// we need to remove `NDM_AUTO_PROVISION_FILTER` to test other provisioner
	s.patchDaemonSet()

	// attach two disks to the target node
	s.attachTwoDisks()
}

func (s *LVMSuite) AfterTest(_, _ string) {
	if s.SSHClient != nil {
		s.SSHClient.Close()
	}
	time.Sleep(5 * time.Second)
}

func (s *LVMSuite) SetupTest() {
	if skipNext {
		s.T().Skip("Skipping test because a previous test failed")
	}
}

func (s *LVMSuite) TearDownTest() {
	if s.T().Failed() {
		skipNext = true
	}
}

func TestLVMOperation(t *testing.T) {
	suite.Run(t, new(LVMSuite))
}

func (s *LVMSuite) cleanupBlockDevices() {
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	bdList, err := bdi.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), nil, err, "List BlockDevices should not get error")

	for _, blockdevice := range bdList.Items {
		err := bdi.Delete(context.TODO(), blockdevice.Name, v1.DeleteOptions{})
		require.Equal(s.T(), nil, err, "Delete BlockDevices should not get error")
	}
}

func (s *LVMSuite) patchDaemonSet() {
	currentDS, err := s.coreClientSet.AppsV1().DaemonSets("harvester-system").Get(context.TODO(), "harvester-node-disk-manager", v1.GetOptions{})
	require.Equal(s.T(), nil, err, "Get DaemonSet should not get error")

	newDS := currentDS.DeepCopy()
	envs := newDS.Spec.Template.Spec.Containers[0].Env
	newEnvs := []corev1.EnvVar{}
	for _, item := range envs {
		if item.Name == "NDM_AUTO_PROVISION_FILTER" {
			continue
		}
		newEnvs = append(newEnvs, item)
	}
	newDS.Spec.Template.Spec.Containers[0].Env = newEnvs
	_, err = s.coreClientSet.AppsV1().DaemonSets("harvester-system").Update(context.TODO(), newDS, v1.UpdateOptions{})
	require.Equal(s.T(), nil, err, "Update DaemonSet should not get error")

	// wait for pod respawn
	time.Sleep(10 * time.Second)
}

func (s *LVMSuite) attachTwoDisks() {
	// Create Target Disk.
	// we can ignore the qcow2 file because we already have the disk files on test_1_disk_hotplug_test.go
	firstDeviceRaw := fmt.Sprintf("%s/node1-sda.qcow2", s.hotplugTargetBaseDir)
	firstDeviceXMLFile := fmt.Sprintf("%s/node1-sda.xml", s.hotplugTargetBaseDir)
	secondDeviceRaw := fmt.Sprintf("%s/node1-sdb.qcow2", s.hotplugTargetBaseDir)
	secondDeviceXMLFile := fmt.Sprintf("%s/node1-sdb.xml", s.hotplugTargetBaseDir)

	disk, err := utils.DiskXMLReader(firstDeviceXMLFile)
	require.Equal(s.T(), nil, err, "Read xml file should not get error")
	disk.Source.File = firstDeviceRaw
	disk.Target.Dev = "sda"
	disk.VENDOR = "HAR_DEV1"
	err = utils.XMLWriter(firstDeviceXMLFile, disk)
	require.Equal(s.T(), nil, err, "Write xml file should not get error")

	s.doAttachDisk(s.hotplugTargetNodeName, firstDeviceXMLFile)

	disk, err = utils.DiskXMLReader(secondDeviceXMLFile)
	require.Equal(s.T(), nil, err, "Read xml file should not get error")
	newWWN := fmt.Sprintf("0x5000c50015%s", utils.GenHash())
	disk.Source.File = secondDeviceRaw
	disk.Target.Dev = "sdb"
	disk.VENDOR = "HAR_DEV2"
	disk.WWN = newWWN
	err = utils.XMLWriter(secondDeviceXMLFile, disk)
	require.Equal(s.T(), nil, err, "Write xml file should not get error")

	s.doAttachDisk(s.hotplugTargetNodeName, secondDeviceXMLFile)

}

func (s *LVMSuite) Test_0_ProvisionLVMWithMultipleDisk() {
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	bdList, err := bdi.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), len(bdList.Items), 2, "BlockdevicesList should only have two devices")
	require.Equal(s.T(), err, nil, "Get BlockdevicesList should not get error")
	require.NotEqual(s.T(), len(bdList.Items), 0, "BlockdevicesList should not be empty")

	for _, blockdevice := range bdList.Items {
		if blockdevice.Spec.NodeName != s.targetNodeName {
			// focus the target node
			continue
		}
		bdStatus := blockdevice.Status
		if bdStatus.DeviceStatus.Details.Vendor == "HAR_DEV1" {
			s.targetDevFirst = blockdevice.DeepCopy()
		} else if bdStatus.DeviceStatus.Details.Vendor == "HAR_DEV2" {
			s.targetDevSecond = blockdevice.DeepCopy()
		}
	}
	require.NotEqual(s.T(), nil, s.targetDevFirst, "targetDevFirst should not be empty")
	require.NotEqual(s.T(), nil, s.targetDevSecond, "targetDevSecond should not be empty")

	targetVGName := "test-vg01"
	// provision first disks
	s.targetDevFirst.Spec.Provision = true
	s.targetDevFirst.Spec.Provisioner = &diskv1.ProvisionerInfo{
		LVM: &diskv1.LVMProvisionerInfo{
			VgName: targetVGName,
		},
	}
	err = s.updateBlockdevice(s.targetDevFirst)
	require.Equal(s.T(), err, nil, "Update BlockDevice(First) should not get error")

	// provision second disks
	s.targetDevSecond.Spec.Provision = true
	s.targetDevSecond.Spec.Provisioner = &diskv1.ProvisionerInfo{
		LVM: &diskv1.LVMProvisionerInfo{
			VgName: targetVGName,
		},
	}
	err = s.updateBlockdevice(s.targetDevSecond)
	require.Equal(s.T(), err, nil, "Update BlockDevice(Second) should not get error")

	// sleep 60 seconds to wait controller handle
	time.Sleep(60 * time.Second)

	targetLVMVG := s.getTargetLVMVG(targetVGName)
	require.NotEqual(s.T(), nil, targetLVMVG, "targetLVM should not be empty")
	require.Equal(s.T(), targetLVMVG.Status.Status, diskv1.VGStatusActive, "LVMVolumeGroup should be Active")
	require.Equal(s.T(), len(targetLVMVG.Status.Devices), 2, "LVMVolumeGroup should have two devices")
	_, found := targetLVMVG.Spec.Devices[s.targetDevFirst.Name]
	require.Equal(s.T(), found, true, "targetDevFirst should be in the LVMVolumeGroup")
	_, found = targetLVMVG.Spec.Devices[s.targetDevSecond.Name]
	require.Equal(s.T(), found, true, "targetDevSecond should be in the LVMVolumeGroup")
}

func (s *LVMSuite) Test_1_RemoveOneDiskOfLVMVolumeGroup() {
	err := error(nil)
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	s.targetDevSecond, err = bdi.Get(context.TODO(), s.targetDevSecond.Name, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get BlockDevice(Second) should not get error")
	// remove second disk from the lvm volume group
	targetVGName := s.targetDevSecond.Spec.Provisioner.LVM.VgName

	s.targetDevSecond.Spec.FileSystem.Provisioned = false
	s.targetDevSecond.Spec.Provision = false
	err = s.updateBlockdevice(s.targetDevSecond)
	require.Equal(s.T(), err, nil, "Update BlockDevice should not get error")

	// sleep 30 seconds to wait controller handle
	time.Sleep(30 * time.Second)
	targetLVMVG := s.getTargetLVMVG(targetVGName)

	_, found := targetLVMVG.Spec.Devices[s.targetDevSecond.Name]
	require.Equal(s.T(), found, false, "targetDevSecond should not be in the LVMVolumeGroup")
}

func (s *LVMSuite) Test_2_ProvisionAnotherLVMVG() {
	err := error(nil)
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	s.targetDevSecond, err = bdi.Get(context.TODO(), s.targetDevSecond.Name, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get BlockDevice(Second) should not get error")
	// Provision another LVM VG
	targetVGName := "test-vg02"
	s.targetDevSecond.Spec.Provision = true
	s.targetDevSecond.Spec.Provisioner = &diskv1.ProvisionerInfo{
		LVM: &diskv1.LVMProvisionerInfo{
			VgName: targetVGName,
		},
	}

	err = s.updateBlockdevice(s.targetDevSecond)
	require.Equal(s.T(), err, nil, "Update BlockDevice should not get error")

	// sleep 30 seconds to wait controller handle
	time.Sleep(30 * time.Second)

	targetLVMVG := s.getTargetLVMVG(targetVGName)
	require.NotEqual(s.T(), targetLVMVG, nil, "targetLVMVG should not be empty")
	require.Equal(s.T(), targetLVMVG.Status.Status, diskv1.VGStatusActive, "LVMVolumeGroup should be Active")

	_, found := targetLVMVG.Spec.Devices[s.targetDevSecond.Name]
	require.Equal(s.T(), found, true, "targetDevSecond should be in the LVMVolumeGroup")
}

func (s *LVMSuite) Test_3_RemoveAllDisksOfLVMVolumeGroup() {
	err := error(nil)
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	s.targetDevFirst, err = bdi.Get(context.TODO(), s.targetDevFirst.Name, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get BlockDevice(First) should not get error")
	s.targetDevSecond, err = bdi.Get(context.TODO(), s.targetDevSecond.Name, v1.GetOptions{})
	require.Equal(s.T(), err, nil, "Get BlockDevice(Second) should not get error")

	// remove all disks from the lvm volume group
	s.targetDevFirst.Spec.FileSystem.Provisioned = false
	s.targetDevFirst.Spec.Provision = false
	err = s.updateBlockdevice(s.targetDevFirst)
	require.Equal(s.T(), err, nil, "Update BlockDevice(First) should not get error")

	s.targetDevSecond.Spec.FileSystem.Provisioned = false
	s.targetDevSecond.Spec.Provision = false
	err = s.updateBlockdevice(s.targetDevSecond)
	require.Equal(s.T(), err, nil, "Update BlockDevice(Second) should not get error")

	// sleep 30 seconds to wait controller handle
	time.Sleep(30 * time.Second)

	lvmClient := s.clientSet.HarvesterhciV1beta1().LVMVolumeGroups("harvester-system")
	lvmList, err := lvmClient.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), err, nil, "Get LVMVolumeGroups should not get error")
	require.Equal(s.T(), len(lvmList.Items), 0, "LVMVolumeGroups should be empty")
}

func (s *LVMSuite) doAttachDisk(nodeName, xmlFile string) {
	cmd := fmt.Sprintf("virsh attach-device --domain %s --file %s --live", nodeName, xmlFile)
	_, _, err := doCommand(cmd)
	require.Equal(s.T(), err, nil, "Running command `%s` should not get error", cmd)

	// wait for controller handling
	time.Sleep(5 * time.Second)
}

func (s *LVMSuite) getTargetLVMVG(vgName string) *diskv1.LVMVolumeGroup {
	lvmClient := s.clientSet.HarvesterhciV1beta1().LVMVolumeGroups("harvester-system")
	lvmList, err := lvmClient.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), err, nil, "Get LVMVolumeGroups should not get error")
	require.NotEqual(s.T(), len(lvmList.Items), 0, "LVMVolumeGroups should not be empty")

	targetLVM := &diskv1.LVMVolumeGroup{}
	// find the LVM and check the status
	for _, lvm := range lvmList.Items {
		if lvm.Spec.NodeName == s.targetNodeName && lvm.Spec.VgName == vgName {
			targetLVM = lvm.DeepCopy()
		}
	}
	return targetLVM
}

func (s *LVMSuite) updateBlockdevice(bd *diskv1.BlockDevice) error {
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	err := error(nil)
	retry := 0
	for retry < 3 {
		_, err = bdi.Update(context.TODO(), bd, v1.UpdateOptions{})
		if err != nil {
			retry++
			time.Sleep(10 * time.Second)
			continue
		}
		return err
	}
	return err
}
