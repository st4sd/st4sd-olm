#!/usr/bin/env bash

dirScripts=`dirname "${0}"`

cd "${dirScripts}/.."

source ${dirScripts}/constants.sh

export PATH=$PATH:`pwd`/bin

export IMAGE_TAG_BASE="quay.io/ibmvassiliad/st4sd-olm-deploy"
export CATALOG_IMG="quay.io/ibmvassiliad/st4sd-olm-deploy-catalog:latest"

operator="st4sd-olm-deploy"
img_base="quay.io/ibmvassiliad/${operator}"
img_bundle="${img_base}-bundle:v${VERSION}"
img_operator="${img_base}:v${VERSION}"

# VV: We use a python script to generate the catalog so here just populate a
# python array out of teh bash array
python_versions=''
for version in ${STABLE_VERSIONS[@]}; do
    python_versions=$(echo -n "${python_versions}'${version}', ")
done

python_versions=$(echo "[${python_versions}]")

export all_bundles=$(python3 <<EOF
versions = ${python_versions}

print(','.join([f"${img_base}-bundle:v{v}" for v in versions]))
EOF
)

echo "Will include following bundles in catalog: ${all_bundles}"

set -xe

# VV: Put together the new bundle. It upgrades ${OLD_VERSION} to ${VERSION}
cp config/manifests/st4sd-olm-deploy.clusterserviceversion.yaml \
   bundle/manifests/

cp config/crd/bases/deploy.st4sd.ibm.com_simulationtoolkits.yaml \
   bundle/manifests/

sed -i '' -e "s#quay.io/st4sd/st4sd-olm-deploy:v%%VERSION%%#${img_operator}#g" \
           -e "s#%%VERSION%%#${VERSION}#g" \
           -e "s#%%OLD_VERSION%%#${OLD_VERSION}#g" \
    bundle/manifests/st4sd-olm-deploy.clusterserviceversion.yaml


# VV: This builds and pushes bundle-${VERSION}
make bundle-build
make bundle-push

# VV: IIRC there was a race condition where pulling the image right after I pushed it
# ended up fetching an older version of the image - this is too strange to be true
# but let's be paranoid for the time being.
echo "Wait 10 seconds before building catalog"
time sleep 10

# VV: Start a new catalog, populate it with ${STABLE_VERSIONS}
rm -rf catalog
mkdir catalog

opm init "${operator}" \
    --default-channel=stable \
    --description=./README.md \
    --icon=logos/operator-icon.svg \
    --output yaml >catalog/index.yaml 

for version in ${STABLE_VERSIONS[@]}; do
    opm render --output yaml "${img_base}-bundle:v${version}" >>catalog/index.yaml
done

echo "---" >>catalog/index.yaml

# VV: This script just builds the upgrade graph. 
# version[i+1] replaces version[i]
# This is good enough for now, in the future we can have a smarter way to build the graph
python3 <<EOF >>catalog/index.yaml
import yaml
versions = ${python_versions}

channel = {
    "schema": "olm.channel",
    "package": "${operator}",
    "name": "stable",
    "entries": [
        {
            "name": f"${operator}.v{versions[i+1]}",
            "replaces": f"${operator}.v{versions[i]}",
        } for i in range(len(versions)-1)
    ]
}

channel['entries'].insert(0, {"name": f"${operator}.v{versions[0]}"})
print(yaml.dump(channel))
EOF

# VV: Sanity check before building the catalog image and pushing it
opm validate catalog

if [[ "$?" -eq "0" ]]; then
    echo "Catalog is valid"
else
    echo "Catalog is broken, fix it"
    exit 1
fi 

docker build -t ${CATALOG_IMG} -f catalog.Dockerfile .
# VV: The above are more or less equivalent to
#     make catalog-build BUNDLE_IMGS="${all_bundles}"
# we basically emulated what --mode=semver does (see makefile)

make catalog-push
