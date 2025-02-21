#!/bin/bash -e

TOP_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/" &> /dev/null && pwd )"

wait_ndm_ready() {
  while [ true ]; do
    running_num=$(kubectl get ds harvester-node-disk-manager -n harvester-system -o 'jsonpath={.status.numberReady}')
    if [[ $running_num -eq ${cluster_nodes} ]]; then
      echo "harvester-node-disk-manager pods are ready!"
      break
    fi
    echo "harvester-node-disk-manager pods are not ready (${running_num}/${cluster_nodes}), sleep 10 seconds."
    sleep 10
  done
}

patch_ndm_auto_provision() {
    kubectl set env daemonset/harvester-node-disk-manager -n harvester-system NDM_AUTO_PROVISION_FILTER="/dev/sd*"
    kubectl rollout restart daemonset/harvester-node-disk-manager -n harvester-system
    sleep 5
}

if [ ! -f $TOP_DIR/kubeconfig ]; then
  echo "kubeconfig does not exist. Please create cluster first."
  echo "Maybe try new_cluster.sh"
  exit 1
fi
echo $TOP_DIR/kubeconfig
export KUBECONFIG=$TOP_DIR/kubeconfig

cluster_nodes=$(yq -e e '.cluster_size' $TOP_DIR/settings.yaml)
echo "cluster nodes: $cluster_nodes"

wait_ndm_ready
patch_ndm_auto_provision
wait_ndm_ready
