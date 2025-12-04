#!/usr/bin/env bash

img_operator=$1
export VERSION=$2

# Example: ./scripts/generate-bundle.sh "quay.io/st4sd/official-base/st4sd-olm:v2.5.4" 0.9.0
echo_usage() {
    echo "Usage: $0 $${img_operator} $${VERSION}"
}

if [[ -z ${img_operator} ]]; then
    echo "Empty img_operator"
    echo_usage
    exit 1
fi

if [[ -z ${VERSION} ]]; then
    echo "Empty VERSION"
    echo_usage
    exit 1
fi

dirScripts=`dirname "${0}"`

cd "${dirScripts}/.."

rm -rf bundle
# VV: Put together the new bundle. It installs/upgrades to ${VERSION}
mkdir -p bundle/manifests
mkdir -p bundle/metadata

cat <<EOF >>bundle/metadata/annotations.yaml
annotations:
  # Core bundle annotations.
  operators.operatorframework.io.bundle.mediatype.v1: registry+v1
  operators.operatorframework.io.bundle.manifests.v1: manifests/
  operators.operatorframework.io.bundle.metadata.v1: metadata/
  operators.operatorframework.io.bundle.package.v1: st4sd-olm
  operators.operatorframework.io.bundle.channels.v1: alpha
  operators.operatorframework.io.metrics.builder: operator-sdk-v1.26.0
  operators.operatorframework.io.metrics.mediatype.v1: metrics+v1
  operators.operatorframework.io.metrics.project_layout: go.kubebuilder.io/v3
EOF

# VV: Ensure CRD is up-to-date
make manifests

cp config/crd/bases/deploy.st4sd.ibm.com_simulationtoolkits.yaml \
   bundle/manifests/

sed -e "s#quay.io/st4sd/official-base/st4sd-olm:v%%VERSION%%#${img_operator}#g" \
           -e "s#%%VERSION%%#${VERSION}#g" \
    config/manifests/st4sd-olm.clusterserviceversion.yaml >bundle/manifests/st4sd-olm.clusterserviceversion.yaml

# VV: This builds st4sd-olm-bundle:${VERSION}
make bundle-build

rm -rf bundles/v${VERSION}
cp -r  bundle bundles/v${VERSION}