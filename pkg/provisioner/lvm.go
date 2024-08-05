package provisioner

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type LVMProvisioner struct {
	*provisioner
	vgName string
}

func NewLVMProvisioner(vgName string, device *diskv1.BlockDevice, blockInfo block.Info) Provisioner {
	baseProvisioner := &provisioner{
		name:      TypeLVM,
		blockInfo: blockInfo,
		device:    device,
	}
	return &LVMProvisioner{
		provisioner: baseProvisioner,
		vgName:      vgName,
	}
}

func (l *LVMProvisioner) GetProvisionerName() string {
	return l.name
}

func (l *LVMProvisioner) Format(_ string) (bool, bool, error) {
	// LVM provisioner does not need format
	return true, false, nil
}

func (l *LVMProvisioner) UnFormat() (bool, error) {
	// LVM provisioner does not need unformat
	return false, nil
}

func (l *LVMProvisioner) Provision() (bool, error) {

	setProvisioned := func() {
		provisionerStatus := &diskv1.ProvisionerStatus{
			Type:   l.name,
			VgName: l.vgName,
		}
		l.device.Status.Provisioner = provisionerStatus
		logrus.Debugf("Set blockdevice CRD (%v) to provisioned", l.device)
		msg := fmt.Sprintf("Added disk %s to volume group %s ", l.device.Name, l.vgName)
		setCondDiskAddedToNodeTrue(l.device, msg, diskv1.ProvisionPhaseProvisioned)
	}

	if l.vgName == "" {
		return false, fmt.Errorf("LVM VG name cannot be empty")
	}
	logrus.Infof("%s provisioning block device %s to vg: %s", l.name, l.device.Name, l.vgName)

	pvsResult, err := getPVScanResult()
	if err != nil {
		return true, fmt.Errorf("failed to get pvscan result. %v", err)
	}
	logrus.Debugf("pvscan result: %v", pvsResult)
	pvFound := false
	vgFound := false
	devPath := l.device.Status.DeviceStatus.DevPath
	for pv, vg := range pvsResult {
		if pv == devPath {
			pvFound = true
			if vg == l.vgName {
				logrus.Debugf("Block device %s is already in VG %s", l.device.Name, l.vgName)
				setProvisioned()
				return false, nil
			}
		}
		if vg == l.vgName {
			vgFound = true
		}
	}

	if !pvFound {
		if err := doPVCreate(devPath); err != nil {
			return true, err
		}
	}
	if !vgFound {
		if err := doVGCreate(devPath, l.vgName); err != nil {
			return true, err
		}
	} else {
		if err := doVGExtend(devPath, l.vgName); err != nil {
			return true, err
		}

	}

	setProvisioned()
	return false, nil
}

func (l *LVMProvisioner) UnProvision() (bool, error) {
	logrus.Infof("%s unprovisioning block device %s from vg: %s", l.name, l.device.Name, l.vgName)

	setUnprovisioned := func() {
		l.device.Status.Provisioner = nil
		l.device.Spec.Provisioner = nil
		logrus.Debugf("Set blockdevice CRD (%v) to unprovisioned", l.device)
		msg := fmt.Sprintf("Removed disk %s from volume group %s ", l.device.Name, l.vgName)
		setCondDiskAddedToNodeFalse(l.device, msg, diskv1.ProvisionPhaseUnprovisioned)
	}

	pvsResult, err := getPVScanResult()
	if err != nil {
		return true, fmt.Errorf("failed to get pvscan result. %v", err)
	}
	logrus.Debugf("pvscan result: %v", pvsResult)
	devPath := l.device.Status.DeviceStatus.DevPath
	pvFound := false
	isInVG := false
	pvCountInVG := 0
	for pv, vg := range pvsResult {
		if pv == devPath {
			pvFound = true
			if vg == l.vgName {
				isInVG = true
				pvCountInVG++
			}
		} else {
			if vg == l.vgName {
				pvCountInVG++
			}
		}
	}

	if !pvFound {
		logrus.Debugf("Block device %s is not in pvs.", l.device.Name)
		setUnprovisioned()
		return false, nil
	}
	if isInVG {
		if pvCountInVG > 1 {
			if err := doVGReduce(devPath, l.vgName); err != nil {
				return true, err
			}
		} else {
			if err := doVGRemove(l.vgName); err != nil {
				return true, err
			}
		}
	}
	if err := doPVRemove(devPath); err != nil {
		return true, err
	}

	setUnprovisioned()
	return false, nil
}

func (l *LVMProvisioner) Update() (bool, error) {
	// Make sure the volume group are all active
	logrus.Infof("%s update block device %s from vg: %s", l.name, l.device.Name, l.vgName)
	if err := doVGActivate(); err != nil {
		return true, err
	}

	return false, nil
}

func getPVScanResult() (map[string]string, error) {
	ns := utils.GetHostNamespacePath(utils.HostProcPath)
	executor, err := utils.NewExecutorWithNS(ns)
	if err != nil {
		return nil, fmt.Errorf("generate executor failed. %v", err)
	}

	args := []string{"--noheadings", "-o", "pv_name,vg_name"}
	output, err := executor.Execute("pvs", args)
	if err != nil {
		return nil, fmt.Errorf("pvs failed. %v", err)
	}
	lines := strings.Split(output, "\n")
	pvScanResult := make(map[string]string)
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// Format should be like: /dev/sda vg01
		pv := fields[0]
		vg := ""
		if len(fields) >= 2 {
			vg = fields[1]
		}
		pvScanResult[pv] = vg
	}
	return pvScanResult, nil
}

func executeCommandWithNS(cmd string, args []string) error {
	ns := utils.GetHostNamespacePath(utils.HostProcPath)
	executor, err := utils.NewExecutorWithNS(ns)
	if err != nil {
		return fmt.Errorf("generate executor failed. %v", err)
	}

	_, err = executor.Execute(cmd, args)
	if err != nil {
		return fmt.Errorf("execute command %s, args: %v failed. %v", cmd, args, err)
	}
	return nil
}

func doPVCreate(devPath string) error {
	return executeCommandWithNS("pvcreate", []string{devPath})
}

func doVGCreate(devPath, vgName string) error {
	return executeCommandWithNS("vgcreate", []string{vgName, devPath})
}

func doVGExtend(devPath, vgName string) error {
	return executeCommandWithNS("vgextend", []string{vgName, devPath})
}

func doVGReduce(devPath, vgName string) error {
	return executeCommandWithNS("vgreduce", []string{vgName, devPath})
}

func doVGRemove(vgName string) error {
	return executeCommandWithNS("vgremove", []string{vgName})
}

func doPVRemove(devPath string) error {
	return executeCommandWithNS("pvremove", []string{devPath})
}

func doVGActivate() error {
	return executeCommandWithNS("vgchange", []string{"--activate", "y"})
}
