#!/usr/bin/env bash

export VERSION="0.1.2"
export OLD_VERSION="0.1.2"

# VV: All versions that go in the "stable" channel, version[i+1] replaces version[i]
STABLE_VERSIONS=(
    "0.0.1" "0.0.2" "0.0.3" "0.0.4" "0.0.5" "0.0.6" "0.0.7" 
    "0.0.8" "0.0.9" "0.0.10" "0.0.11" "0.0.12" "0.0.13"
    "0.0.14" "0.0.15" "0.0.16" "0.0.17" "0.0.18" "0.1.1" "0.1.2")
