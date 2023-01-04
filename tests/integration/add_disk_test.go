package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinburke/ssh_config"
	"github.com/melbahja/goph"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	clientset "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned"
)

type Suite struct {
	suite.Suite
	SSHClient      *goph.Client
	clientSet      *clientset.Clientset
	targetNodeName string
}

type ProvisionedDisk struct {
	devPath string
	UUID    string
}

func (s *Suite) SetupSuite() {
	nodeName := ""
	f, _ := os.Open(filepath.Join(os.Getenv("HOME"), "ssh-config"))
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
	privateKey := filepath.Join(os.Getenv("HOME"), splitedResult[len(splitedResult)-1])
	// Start new ssh connection with private key.
	auth, err := goph.Key(privateKey, "")
	require.Equal(s.T(), err, nil, "generate ssh auth key should not get error")

	s.SSHClient, err = goph.NewUnknown(targetUser, targetHost, auth)
	require.Equal(s.T(), err, nil, "New ssh connection should not get error")

	kubeconfig := filepath.Join(os.Getenv("HOME"), "kubeconfig")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	require.Equal(s.T(), err, nil, "Generate kubeconfig should not get error")

	s.clientSet, err = clientset.NewForConfig(config)
	require.Equal(s.T(), err, nil, "New clientset should not get error")

}

func (s *Suite) AfterTest(_, _ string) {
	if s.SSHClient != nil {
		s.SSHClient.Close()
	}
}

func TestAddDisks(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (s *Suite) TestAddSingleDisk() {
	// prepare to check the added disk
	var provisionedDisk ProvisionedDisk
	bdi := s.clientSet.HarvesterhciV1beta1().BlockDevices("longhorn-system")
	bdList, err := bdi.List(context.TODO(), v1.ListOptions{})
	require.Equal(s.T(), err, nil, "Get BlockdevicesList should not get error")
	for _, blockdevice := range bdList.Items {
		if blockdevice.Spec.NodeName != s.targetNodeName {
			// focus the target node
			continue
		}
		bdStatus := blockdevice.Status
		if bdStatus.State == "Active" && bdStatus.ProvisionPhase == "Provisioned" {
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
