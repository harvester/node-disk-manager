package volumegroup

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/lvm"
	"github.com/harvester/node-disk-manager/pkg/option"
)

type Controller struct {
	namespace string
	nodeName  string

	LVMVolumeGroupCache ctldiskv1.LVMVolumeGroupCache
	LVMVolumeGroups     ctldiskv1.LVMVolumeGroupController
}

const (
	lvmVGHandlerName = "harvester-lvm-volumegroup-handler"
)

func Register(ctx context.Context, lvmVGs ctldiskv1.LVMVolumeGroupController, opt *option.Option) error {

	c := &Controller{
		namespace:           opt.Namespace,
		nodeName:            opt.NodeName,
		LVMVolumeGroups:     lvmVGs,
		LVMVolumeGroupCache: lvmVGs.Cache(),
	}

	c.LVMVolumeGroups.OnChange(ctx, lvmVGHandlerName, c.OnLVMVGChange)
	c.LVMVolumeGroups.OnRemove(ctx, lvmVGHandlerName, c.OnLVMVGRemove)
	return nil
}

func (c *Controller) OnLVMVGChange(_ string, lvmVG *diskv1.LVMVolumeGroup) (*diskv1.LVMVolumeGroup, error) {
	if lvmVG == nil || lvmVG.DeletionTimestamp != nil {
		logrus.Infof("Skip this round because lvm volume group is deleted or deleting")
		return nil, nil
	}

	if lvmVG.Spec.NodeName != c.nodeName {
		logrus.Infof("Skip this round because lvm volume group is not belong to this node")
		return nil, nil
	}

	logrus.Infof("Prepare to handle LVMVolumeGroup %s changed: %v", lvmVG.Name, lvmVG)

	switch lvmVG.Spec.DesiredState {
	case diskv1.VGStateEnabled:
		logrus.Infof("Prepare to enable LVMVolumeGroup %s", lvmVG.Name)
		return c.updateEnabledLVMVolumeGroup(lvmVG)
	case diskv1.VGStateDisabled:
		// should only called manually set the VGState to Disabled
		logrus.Infof("Prepare to disable LVMVolumeGroup %s", lvmVG.Name)
		return c.disableLVMVolumeGroup(lvmVG)
	}
	return nil, nil
}

func (c *Controller) OnLVMVGRemove(_ string, lvmVG *diskv1.LVMVolumeGroup) (*diskv1.LVMVolumeGroup, error) {
	if lvmVG == nil || lvmVG.DeletionTimestamp != nil {
		// make sure the volume group is already deleted
		logrus.Infof("Ensure the lvm volume group is already deleted if the lvmVG CR is nil")
		return c.removeLVMVolumeGroup(lvmVG)
	}

	return nil, nil
}

func (c *Controller) updateEnabledLVMVolumeGroup(lvmVG *diskv1.LVMVolumeGroup) (*diskv1.LVMVolumeGroup, error) {
	logrus.Infof("Enable LVMVolumeGroup %s", lvmVG.Name)

	pvsResult, err := lvm.GetPVScanResult()
	if err != nil {
		return nil, fmt.Errorf("failed to get pvscan result. %v", err)
	}
	logrus.Debugf("pvscan result: %v", pvsResult)
	currentDevs := map[string]string{}
	if lvmVG.Status != nil && lvmVG.Status.Devices != nil && len(lvmVG.Status.Devices) > 0 {
		currentDevs = lvmVG.Status.Devices
	}
	if maps.Equal(currentDevs, lvmVG.Spec.Devices) {
		logrus.Info("Skip this round because the devices are not changed")
		return nil, nil
	}
	lvmVGCpy := lvmVG.DeepCopy()
	if lvmVGCpy.Status == nil {
		lvmVGCpy.Status = &diskv1.VolumeGroupStatus{}
	}

	if lvmVG.Status != nil && len(lvmVG.Status.Devices) == 0 {
		logrus.Warnf("No devices found in LVMVolumeGroup %s, skip", lvmVG.Name)
		return nil, nil
	}
	// update devices
	toAdd := getToAddDevs(lvmVG.Spec.Devices, currentDevs)
	toRemove := getToRemoveDevs(lvmVG.Spec.Devices, currentDevs)
	err = updatePVAndVG(lvmVGCpy, toAdd, toRemove, pvsResult)
	if err != nil {
		return nil, err
	}

	vgConds := diskv1.VolumeGroupCondition{
		Type:               diskv1.VGConditionReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "Volume Group is Ready",
		Message:            fmt.Sprintf("Volume Group is Ready with devices %v", lvmVG.Spec.Devices),
	}
	newConds := UpdateLVMVGsConds(lvmVGCpy.Status.VGConditions, vgConds)
	lvmVGCpy.Status.VGConditions = newConds
	lvmVGCpy.Status.Status = diskv1.VGStatusActive
	if !reflect.DeepEqual(lvmVG, lvmVGCpy) {
		return c.LVMVolumeGroups.UpdateStatus(lvmVGCpy)
	}
	return nil, nil
}

func (c *Controller) disableLVMVolumeGroup(lvmVG *diskv1.LVMVolumeGroup) (*diskv1.LVMVolumeGroup, error) {
	logrus.Infof("Disable LVMVolumeGroup %s", lvmVG.Spec.VgName)
	err := lvm.DoVGDeactivate(lvmVG.Spec.VgName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logrus.Infof("VolumeGroup %s is not found, skip", lvmVG.Spec.VgName)
		} else {
			return nil, fmt.Errorf("failed to remove VG %s. %v", lvmVG.Spec.VgName, err)
		}
	}
	return nil, nil
}

func (c *Controller) removeLVMVolumeGroup(lvmVG *diskv1.LVMVolumeGroup) (*diskv1.LVMVolumeGroup, error) {
	logrus.Infof("Remove LVMVolumeGroup %s", lvmVG.Name)
	err := lvm.DoVGRemove(lvmVG.Spec.VgName, false)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			logrus.Infof("VolumeGroup %s is not found, skip", lvmVG.Spec.VgName)
		} else {
			return nil, fmt.Errorf("failed to remove VG %s. %v", lvmVG.Spec.VgName, err)
		}
	}

	return nil, nil
}

func checkPVAndVG(pvsResult map[string]string, targetPV, targetVG string) (pvFound, vgFound bool, pvCount int) {
	pvCount = 0
	for pv, vg := range pvsResult {
		if pv == targetPV {
			pvFound = true
			if vg == targetVG {
				pvCount++
				vgFound = true
				continue
			}
		}
		if vg == targetVG {
			vgFound = true
			pvCount++
		}
	}
	return
}

func UpdateLVMVGsConds(curConds []diskv1.VolumeGroupCondition, c diskv1.VolumeGroupCondition) []diskv1.VolumeGroupCondition {
	found := false
	var pod = 0
	logrus.Infof("Prepare to check the coming Type: %s, Status: %s", c.Type, c.Status)
	for id, cond := range curConds {
		if cond.Type == c.Type {
			found = true
			pod = id
			break
		}
	}

	if found {
		curConds[pod] = c
	} else {
		curConds = append(curConds, c)
	}
	return curConds

}

func updatePVAndVG(vgCpy *diskv1.LVMVolumeGroup, toAdd, toRemove map[string]string, pvsResult map[string]string) error {
	logrus.Infof("Prepare to add devices: %v", toAdd)
	for bdName, dev := range toAdd {
		pvFound, vgFound, _ := checkPVAndVG(pvsResult, dev, vgCpy.Spec.VgName)
		logrus.WithFields(logrus.Fields{
			"device":  dev,
			"vgName":  vgCpy.Spec.VgName,
			"pvFound": pvFound,
			"vgFound": vgFound,
		}).Infof("Checking for PV and VG")
		if !vgFound {
			if err := lvm.DoVGCreate(dev, vgCpy.Spec.VgName); err != nil {
				return err
			}
		}
		if !pvFound {
			if err := lvm.DoPVCreate(dev); err != nil {
				return err
			}
			if err := lvm.DoVGExtend(dev, vgCpy.Spec.VgName); err != nil {
				return err
			}
		}
		if vgCpy.Status.Devices == nil {
			vgCpy.Status.Devices = map[string]string{}
		}
		vgCpy.Status.Devices[bdName] = dev
	}
	logrus.Infof("Prepare to remove devices: %v", toRemove)
	for bdName, dev := range toRemove {
		pvFound, vgFound, pvInVGCounts := checkPVAndVG(pvsResult, dev, vgCpy.Spec.VgName)
		logrus.WithFields(logrus.Fields{
			"device":       dev,
			"vgName":       vgCpy.Spec.VgName,
			"pvFound":      pvFound,
			"vgFound":      vgFound,
			"pvInVGCounts": pvInVGCounts,
		}).Infof("Checking for PV and VG")
		if !pvFound {
			logrus.Infof("Block device %s is not in pvs, return directly!", bdName)
			return nil
		}
		// handle if vg is found
		if vgFound {
			if pvInVGCounts > 1 {
				if err := lvm.DoVGReduce(dev, vgCpy.Spec.VgName); err != nil {
					return err
				}
			} else {
				if err := lvm.DoVGRemove(vgCpy.Spec.VgName, false); err != nil {
					return err
				}
			}
		}
		lvm.DoPVRemove(dev)
		delete(vgCpy.Status.Devices, bdName)
	}
	return nil
}

func getToAddDevs(specDevs, currentDevs map[string]string) map[string]string {
	toAdd := map[string]string{}
	for bdName, dev := range specDevs {
		if _, found := currentDevs[bdName]; !found {
			toAdd[bdName] = dev
		}
	}
	return toAdd
}

func getToRemoveDevs(specDevs, currentDevs map[string]string) map[string]string {
	toRemove := map[string]string{}
	for bdName := range currentDevs {
		if _, found := specDevs[bdName]; !found {
			toRemove[bdName] = currentDevs[bdName]
		}
	}
	return toRemove
}
