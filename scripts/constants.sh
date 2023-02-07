#!/usr/bin/env bash

# VV: All the images in the release will have this tag
export ST4SD_COLLECTION_IMAGES_VERSION="bundle-2.0.0-alpha6"
export VERSION="0.0.8"
export OLD_VERSION="0.0.7"

# VV: All versions that go in the "stable" channel, version[i+1] replaces version[i]
STABLE_VERSIONS=("0.0.1" "0.0.2" "0.0.3" "0.0.4" "0.0.5" "0.0.6" "0.0.7" "0.0.8")
