#!/bin/bash -e

source helpers.sh

ensure_longhorn_ready

pushd $TOP_DIR
cat >> ndm-override.yaml.default << 'EOF'
debug: true
EOF

if [ ! -f ndm-override.yaml ]; then
  mv ndm-override.yaml.default ndm-override.yaml
fi

cp -r ../deploy/charts/harvester-node-disk-manager harvester-node-disk-manager

target_img=$(yq -e .image.repository ndm-override.yaml)
echo "install target image: ${target_img}"
$HELM install -f $TOP_DIR/ndm-override.yaml harvester-node-disk-manager ./harvester-node-disk-manager --create-namespace -n harvester-system

wait_ndm_ready
echo "harvester-node-disk-manager is ready"
popd