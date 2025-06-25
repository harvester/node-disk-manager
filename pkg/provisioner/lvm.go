package provisioner

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/lvm"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type LVMProvisioner struct {
	*provisioner
	vgName   string
	nodeName string
	vgClient ctldiskv1.LVMVolumeGroupController
	lock     *sync.Mutex
}

func NewLVMProvisioner(vgName, nodeName string, lvmVGs ctldiskv1.LVMVolumeGroupController, device *diskv1.BlockDevice, blockInfo block.Info, lock *sync.Mutex) (Provisioner, error) {
	baseProvisioner := &provisioner{
		name:      TypeLVM,
		blockInfo: blockInfo,
		device:    device,
	}
	return &LVMProvisioner{
		provisioner: baseProvisioner,
		vgName:      vgName,
		vgClient:    lvmVGs,
		nodeName:    nodeName,
		lock:        lock,
	}, nil
}

func (l *LVMProvisioner) GetProvisionerName() string {
	return l.name
}

// Format operation on the LVM use to ensure the device is clean and ready to be used by LVM.
func (l *LVMProvisioner) Format(devPath string) (isFormatComplete, isRequeueNeeded bool, err error) {
	// if the pv is created, skip wipefs.
	// Because the device is already in use, wipefs will break the device.
	pvResult, err := lvm.GetPVScanResult()
	if err != nil {
		return false, true, err
	}
	if _, found := pvResult[devPath]; found {
		return true, false, nil
	}
	logrus.Infof("Wipe the device %s", devPath)
	if _, err := utils.NewExecutor().Execute("wipefs", []string{"-a", devPath}); err != nil {
		return false, true, err
	}
	return true, false, nil
}

func (l *LVMProvisioner) UnFormat() (bool, error) {
	// LVM provisioner does not need unformat
	return false, nil
}

// Provision creates (if needed) a LVMVolumeGroup CRD and update the corresponding fields.
func (l *LVMProvisioner) Provision() (bool, error) {
	logrus.Infof("Provisioning block device %s to vg: %s", l.device.Name, l.vgName)
	found := true
	// because the LVMVG name is a generated name, we need to lock here to ensure we only have one LVMVG CRD for specific vgName.
	l.lock.Lock()
	defer l.lock.Unlock()
	lvmvg, err := l.getTargetLVMVG()
	if err != nil {
		if !errors.IsNotFound(err) {
			return true, err
		}
		found = false
	}
	requeue, err := l.addDevOrCreateLVMVgCRD(lvmvg, found)
	if err != nil {
		return requeue, err
	}

	// first round the lvmvg must be nil, so we need to check it.
	if lvmvg != nil && lvmvg.Status != nil && lvmvg.Status.Status == diskv1.VGStatusActive {
		setCondDiskAddedToNodeTrue(l.device, fmt.Sprintf("Added disk %s to volume group %s ", l.device.Name, l.vgName), diskv1.ProvisionPhaseProvisioned)
		return false, nil
	}
	return true, nil
}

// UnProvision update the LVMVolumeGroup CRD and remove the LVMVolumeGroup CRD if the device is the last one in the VG.
func (l *LVMProvisioner) UnProvision() (bool, error) {
	logrus.Infof("Unprovisioning block device %s from vg: %s", l.device.Name, l.vgName)
	lvmvg, err := l.getTargetLVMVG()
	if err != nil {
		if errors.IsNotFound(err) {
			// do nothing if the LVMVolumeGroup CRD is not found
			logrus.Warn("CR LVMVolumeGroup is not found, skip UnProvision")
			msg := fmt.Sprintf("Removed disk %s from volume group %s ", l.device.Name, l.vgName)
			setCondDiskAddedToNodeFalse(l.device, msg, diskv1.ProvisionPhaseUnprovisioned)
			return false, nil
		}
		return true, err
	}
	logrus.Infof("%s unprovisioning block device %s from vg: %s", l.name, l.device.Name, l.vgName)
	requeue, err := l.removeDevFromLVMVgCRD(lvmvg, l.device.Name)
	if err != nil {
		return requeue, err
	}
	if lvmvg.Status != nil {
		if _, found := lvmvg.Status.Devices[l.device.Name]; !found {
			msg := fmt.Sprintf("Removed disk %s from volume group %s ", l.device.Name, l.vgName)
			setCondDiskAddedToNodeFalse(l.device, msg, diskv1.ProvisionPhaseUnprovisioned)
			return false, nil
		}
	}
	// waiting the device removed from the LVMVolumeGroup CRD
	logrus.Infof("Waiting for the device %s removed from the LVMVolumeGroup CRD %v", l.device.Name, lvmvg)
	return true, nil
}

func (l *LVMProvisioner) Update() (requeue bool, err error) {
	// Update DesiredState to Reconciling
	logrus.Infof("Prepare to Update LVMVolumeGroup %s", l.vgName)
	lvmvg, err := l.getTargetLVMVG()
	if err != nil {
		if errors.IsNotFound(err) {
			return true, fmt.Errorf("failed to get LVMVolumeGroup %s, err: %v", l.vgName, err)
		}
		return true, err
	}

	if lvmvg.Spec.DesiredState == diskv1.VGStateEnabled {
		// make sure the volume group is active
		err := lvm.DoVGActivate(lvmvg.Spec.VgName)
		if err != nil {
			return true, fmt.Errorf("failed to activate volume group %s, err: %v", l.vgName, err)
		}
	} else if lvmvg.Spec.DesiredState == diskv1.VGStateDisabled {
		// make sure the volume group is inactive
		logrus.Infof("Should not go here, because the LVMVolumeGroup %s should not be disabled", l.vgName)
	}
	return
}

func (l *LVMProvisioner) addDevOrCreateLVMVgCRD(lvmVG *diskv1.LVMVolumeGroup, found bool) (requeue bool, err error) {
	logrus.Infof("addDevOrCreateLVMVgCRD: %v, found: %v", lvmVG, found)
	requeue = false
	err = nil
	if !found {
		lvmVG = &diskv1.LVMVolumeGroup{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: fmt.Sprintf("%s-", l.vgName),
				Namespace:    utils.HarvesterNS,
				Labels: map[string]string{
					lvm.LVMTopoKeyNode: l.nodeName,
				},
			},
			Spec: diskv1.VolumeGroupSpec{
				NodeName:     l.nodeName,
				VgName:       l.vgName,
				DesiredState: diskv1.VGStateEnabled,
				Devices:      map[string]string{l.device.Name: l.device.Status.DeviceStatus.DevPath},
			},
		}
		if _, err = l.vgClient.Create(lvmVG); err != nil {
			requeue = true
			logrus.Infof("[DEBUG]: error: %v", err)
			err = fmt.Errorf("failed to create LVMVolumeGroup %s. %v", l.vgName, err)
			return
		}
		logrus.Infof("Created LVMVolumeGroup %s, content: %v", l.vgName, lvmVG)
		return
	}
	if lvmVG == nil {
		requeue = true
		err = fmt.Errorf("failed to get LVMVolumeGroup %s, but notFound is False", l.vgName)
		return
	}
	if lvmVG.Spec.Devices == nil {
		lvmVG.Spec.Devices = make(map[string]string)
	}
	if _, found := lvmVG.Spec.Devices[l.device.Name]; found {
		logrus.Infof("Skip this round because the devices are not changed")
		return
	}
	lvmVGCpy := lvmVG.DeepCopy()
	if lvmVGCpy.Spec.Devices == nil {
		lvmVGCpy.Spec.Devices = make(map[string]string)
	}
	lvmVGCpy.Spec.Devices[l.device.Name] = l.device.Status.DeviceStatus.DevPath
	if !reflect.DeepEqual(lvmVG, lvmVGCpy) {
		if _, err = l.vgClient.Update(lvmVGCpy); err != nil {
			requeue = true
			err = fmt.Errorf("failed to update LVMVolumeGroup %s. %v", l.vgName, err)
			return
		}
		logrus.Infof("Updated LVMVolumeGroup %s, content: %v", l.vgName, lvmVGCpy)
	}
	return
}

func (l *LVMProvisioner) removeDevFromLVMVgCRD(lvmVG *diskv1.LVMVolumeGroup, targetDevice string) (requeue bool, err error) {
	logrus.Infof("removeDevFromLVMVG %s, devices before remove: %v", lvmVG.Spec.VgName, lvmVG.Spec.Devices)
	requeue = false
	err = nil

	lvmVGCpy := lvmVG.DeepCopy()
	delete(lvmVGCpy.Spec.Devices, targetDevice)
	logrus.Debugf("New devices (after remove %v): %v", targetDevice, lvmVGCpy.Spec.Devices)
	if len(lvmVGCpy.Status.Devices) == 0 {
		if err = l.vgClient.Delete(lvmVGCpy.Namespace, lvmVGCpy.Name, &metav1.DeleteOptions{}); err != nil {
			requeue = true
			err = fmt.Errorf("failed to delete LVMVolumeGroup %s. %v", l.vgName, err)
			return
		}
		logrus.Infof("Deleted LVMVolumeGroup %s", l.vgName)
		return
	}
	// we need to wait the device
	if !reflect.DeepEqual(lvmVG, lvmVGCpy) {
		if _, err = l.vgClient.Update(lvmVGCpy); err != nil {
			requeue = true
			err = fmt.Errorf("failed to update LVMVolumeGroup %s. %v", l.vgName, err)
			return
		}
	}
	logrus.Infof("Updated LVMVolumeGroup %s, content: %v", l.vgName, lvmVGCpy)
	return
}

func (l *LVMProvisioner) getTargetLVMVG() (target *diskv1.LVMVolumeGroup, err error) {
	found := false
	// check if the LVMVolumeGroup CRD is already provisioned
	selector, err := lvm.GenerateSelector(l.nodeName)
	if err != nil {
		err = fmt.Errorf("failed to generate selector: %v", err)
		return
	}
	lvmvgs, err := l.vgClient.List(utils.HarvesterNS, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		err = fmt.Errorf("failed to list LVMVolumeGroup %s. %v", l.vgName, err)
		return
	}
	for _, lvmvg := range lvmvgs.Items {
		if lvmvg.Spec.NodeName == l.nodeName && lvmvg.Spec.VgName == l.vgName {
			target = lvmvg.DeepCopy()
			found = true
			break
		}
	}
	if !found {
		err = errors.NewNotFound(diskv1.Resource("lvmvolumegroups"), l.vgName)
	}
	return
}
