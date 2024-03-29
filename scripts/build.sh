#!/usr/bin/env bash

export IMAGE_PUSH=${IMAGE_PUSH:-yes}
set -xe
dirScripts=`dirname "${0}"`

cd "${dirScripts}/.."
source scripts/constants.sh

if [[ ! -z ${OVERRIDE_VERSION} ]]; then
  echo "Overriding version to ${OVERRIDE_VERSION}"
  export VERSION=${OVERRIDE_VERSION}
fi

export IMAGE_TAG_BASE=${IMAGE_TAG_BASE:-"quay.io/st4sd/official-base/st4sd-olm"}

make docker-build

if [[ "${IMAGE_PUSH}" == "yes" ]]; then
  make docker-push
else
  echo "Will not push image because \${IMAGE_PUSH}=${IMAGE_PUSH} (!= yes)"
fi
