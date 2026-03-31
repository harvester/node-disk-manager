#!/bin/bash -e

TOP_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/" &> /dev/null && pwd )"

HELM_VERSION=v3.20.0
HELM_SUM_amd64=dbb4c8fc8e19d159d1a63dda8db655f9ffa4aac1b9a6b188b34a40957119b286
HELM_SUM_arm64=bfb14953295d5324d47ab55f3dfba6da28d46c848978c8fbf412d4271bdc29f1
HELM_SUM="HELM_SUM_${ARCH}"

# ensure helm command
if [[ $(ensure_command helm) -eq 1 ]]; then
    echo "no helm, try to curl..."
    curl -O https://get.helm.sh/helm-${HELM_VERSION}-linux-${ARCH}.tar.gz
    echo "${!HELM_SUM}" helm-${HELM_VERSION}-linux-${ARCH}.tar.gz | sha256sum -c -
    tar xvzf helm-${HELM_VERSION}-linux-${ARCH}.tar.gz
    HELM=$TOP_DIR/linux-amd64/helm
    $HELM version
else
    echo "Get helm, version info as below"
    HELM=$(which helm)
    $HELM version
fi


# configure kubeconfig
if [ ! -f $TOP_DIR/kubeconfig ]; then
  echo "kubeconfig does not exist. Please create cluster first."
  echo "Maybe try new_cluster.sh"
  exit 1
fi
echo $TOP_DIR/kubeconfig
export KUBECONFIG=$TOP_DIR/kubeconfig

cluster_nodes=$(yq -e e '.cluster_size' $TOP_DIR/settings.yaml)

ensure_command() {
  local cmd=$1
  if ! which $cmd &> /dev/null; then
    echo 1
    return
  fi
  echo 0
}

wait_ndm_ready() {
  while [ true ]; do
    running_num=$(kubectl get ds harvester-node-disk-manager -n harvester-system -o 'jsonpath={.status.numberReady}')
    if [[ $running_num -eq ${cluster_nodes} ]]; then
      echo "harvester-node-disk-manager pods are ready!"
      break
    fi
    echo "harvester-node-disk-manager pods are not ready, sleep 10 seconds."
    sleep 10
  done
}

ensure_longhorn_ready() {
  # ensure longhorn-manager first
  while [ true ]; do
    running_num=$(kubectl get ds longhorn-manager -n longhorn-system -o 'jsonpath={.status.numberReady}')
    if [[ $running_num -eq ${cluster_nodes} ]]; then
      echo "longhorn-manager pods are ready!"
      break
    fi
    echo "check longhorn-manager failure, please deploy longhorn first."
    exit 1
  done

  # ensure instance-manager-e ready
  while [ true ]; do
    running_num=$(kubectl get pods -n longhorn-system |grep ^instance-manager |grep Running |awk '{print $3}' |wc -l)
    if [[ $running_num -eq ${cluster_nodes} ]]; then
      echo "instance-manager pods are ready!"
      break
    fi
    echo "check instance-manager failure, please deploy longhorn first."
    exit 1
  done
}

patch_ndm_auto_provision() {
    kubectl set env daemonset/harvester-node-disk-manager -n harvester-system NDM_AUTO_PROVISION_FILTER="/dev/sd*"
    kubectl rollout restart daemonset/harvester-node-disk-manager -n harvester-system
    sleep 5
}