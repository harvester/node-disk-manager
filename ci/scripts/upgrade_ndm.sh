#!/bin/bash -e

source helpers.sh

pushd $TOP_DIR
# cleanup first
rm -rf harvester-node-disk-manager*
rm -rf ndm-override.yaml

cp -r ../deploy/charts/harvester-node-disk-manager harvester-node-disk-manager
cp ../ci/charts/ndm-override.yaml ndm-override.yaml

target_img=$(yq -e .image.repository ndm-override.yaml)
echo "upgrade target image: ${target_img}, upgrading ..."
$HELM upgrade -f $TOP_DIR/ndm-override.yaml harvester-node-disk-manager harvester-node-disk-manager/ -n harvester-system

sleep 30 # wait 30 seconds for ndm respawn pods

wait_ndm_ready
# check image
pod_name=$(kubectl get pods -n harvester-system |grep Running |grep -v webhook |grep ^harvester-node-disk-manager|head -n1 |awk '{print $1}')
container_img=$(kubectl get pods ${pod_name} -n harvester-system -o yaml |yq -e .spec.containers[0].image |tr ":" \n)
yaml_img=$(yq -e .image.repository ndm-override.yaml)
if grep -q ${yaml_img} <<< ${container_img}; then
  echo "Image is equal: ${yaml_img}"
else
  echo "Image is non-equal, container: ${container_img}, yaml file: ${yaml_img}"
  exit 1
fi
echo "harvester-node-disk-manager upgrade successfully!"
popd