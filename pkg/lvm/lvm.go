package lvm

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/harvester/node-disk-manager/pkg/utils"
)

const (
	LVMTopoKeyNode = "topology.lvm.csi/node"
)

func GetPVScanResult() (map[string]string, error) {
	ns := utils.GetHostNamespacePath(utils.HostProcPath)
	executor, err := utils.NewExecutorWithNS(ns)
	if err != nil {
		return nil, fmt.Errorf("generate executor failed. %v", err)
	}

	args := []string{"--noheadings", "-o", "pv_name,vg_name"}
	output, err := executor.Execute("pvs", args)
	if err != nil {
		return nil, fmt.Errorf("failed to execute 'pvs' command: %v", err)
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
		return fmt.Errorf("generate executor failed: %v", err)
	}

	_, err = executor.Execute(cmd, args)
	if err != nil {
		return fmt.Errorf("execute command '%s' with args '%v' failed: %v", cmd, args, err)
	}
	return nil
}

func DoPVCreate(devPath string) error {
	return executeCommandWithNS("pvcreate", []string{devPath})
}

func DoVGCreate(devPath, vgName string) error {
	return executeCommandWithNS("vgcreate", []string{vgName, devPath})
}

func DoVGExtend(devPath, vgName string) error {
	return executeCommandWithNS("vgextend", []string{vgName, devPath})
}

func DoVGReduce(devPath, vgName string) error {
	return executeCommandWithNS("vgreduce", []string{vgName, devPath})
}

func DoVGRemove(vgName string) error {
	return executeCommandWithNS("vgremove", []string{vgName})
}

func DoPVRemove(devPath string) error {
	return executeCommandWithNS("pvremove", []string{devPath})
}

func DoVGActivate(vgName string) error {
	return executeCommandWithNS("vgchange", []string{"--activate", "y", vgName})
}

func DoVGDeactive(vgName string) error {
	return executeCommandWithNS("vgchange", []string{"--activate", "n", vgName})
}

func GenerateSelector(nodeName string) (labels.Selector, error) {
	nodeReq, err := labels.NewRequirement(LVMTopoKeyNode, selection.Equals, []string{nodeName})
	if err != nil {
		return nil, fmt.Errorf("error creating requirement: %v", err)
	}
	lvmVGSelector := labels.NewSelector()
	lvmVGSelector = lvmVGSelector.Add(*nodeReq)
	return lvmVGSelector, nil
}
