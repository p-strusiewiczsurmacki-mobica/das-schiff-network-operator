#!/usr/bin/env bash
# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# borrowed from https://github.com/kubernetes-sigs/cluster-api/hack/vershion.sh with minor changes

set -o errexit
set -o nounset
set -o pipefail

if [[ "${TRACE-0}" == "1" ]]; then
    set -o xtrace
fi

version::get_version_vars() {
    # shellcheck disable=SC1083
    GIT_COMMIT="$(git rev-parse HEAD^{commit})"

    if git_status=$(git status --porcelain 2>/dev/null) && [[ -z ${git_status} ]]; then
        GIT_TREE_STATE="clean"
    else
        GIT_TREE_STATE="dirty"
    fi

    # stolen from k8s.io/hack/lib/version.sh
    # Use git describe to find the version based on annotated tags.
    if [[ -n ${GIT_VERSION-} ]] || GIT_VERSION=$(git describe --tags --abbrev=14 --match "v[0-9]*" 2>/dev/null); then
        if [[ "${GIT_TREE_STATE}" == "dirty" ]]; then
            # git describe --dirty only considers changes to existing files, but
            # that is problematic since new untracked .go files affect the build,
            # so use our idea of "dirty" from git status instead.
            GIT_VERSION+="-dirty"
        fi


        # Try to match the "git describe" output to a regex to try to extract
        # the "major" and "minor" versions and whether this is the exact tagged
        # version or whether the tree is between two tagged versions.
        if [[ "${GIT_VERSION}" =~ ^v([0-9]+)\.([0-9]+)(\.[0-9]+)?([-].*)?([+].*)?$ ]]; then
            GIT_MAJOR=${BASH_REMATCH[1]}
            GIT_MINOR=${BASH_REMATCH[2]}
        fi

        # If GIT_VERSION is not a valid Semantic Version, then refuse to build.
        if ! [[ "${GIT_VERSION}" =~ ^v([0-9]+)\.([0-9]+)(\.[0-9]+)?(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]; then
            echo "GIT_VERSION should be a valid Semantic Version. Current value: ${GIT_VERSION}"
            echo "Please see more details here: https://semver.org"
            exit 1
        fi
    fi

    GIT_RELEASE_TAG=$(git describe --abbrev=0 --tags)
    GIT_RELEASE_COMMIT=$(git rev-list -n 1  "${GIT_RELEASE_TAG}")
}

# stolen from k8s.io/hack/lib/version.sh and modified
# Prints the value that needs to be passed to the -ldflags parameter of go build
version::ldflags() {
    version::get_version_vars

    local -a ldflags
    function add_ldflag() {
        local key=${1}
        local val=${2}
        ldflags+=(
            "-X 'github.com/telekom/das-schiff-network-operator/pkg/version.${key}=${val}'"
        )
    }

    add_ldflag "gitCommit" "${GIT_COMMIT}"
    add_ldflag "gitVersion" "${GIT_VERSION}"

    # The -ldflags parameter takes a single string, so join the output.
    echo "${ldflags[*]-}"
}

version::ldflags