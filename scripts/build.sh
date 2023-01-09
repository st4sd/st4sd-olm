#!/usr/bin/env bash

dirScripts=`dirname "${0}"`

cd "${dirScripts}/.."
source ${dirScripts}/constants.sh

set -xe

export IMAGE_TAG_BASE="quay.io/ibmvassiliad/st4sd-olm-deploy"

make docker-build 
make docker-push

# img_bundle="${IMAGE_TAG_BASE}-bundle:v${VERSION}"
## We only need to run this step once
## make bundle

# # docker build -f bundle.Dockerfile -t ${img_bundle} . && docker push ${img_bundle}
# make bundle-build
# make bundle-push

# make catalog-build
# make catalog-push