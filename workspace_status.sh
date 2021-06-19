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

set -o errexit
set -o nounset
set -o pipefail

#`p_` takes two arguments to define a bazel workspace status variable:
#
#  * the name of the variable
#  * a default value
#
# If an environment variable with the corresponding name is set, its value is
# used. Otherwise, the provided default value is used.
p_() {
    if (( $# == 2 )); then
        echo "$1" "${!1:-$2}"
    else
        return 1
    fi
}

build_date="$(date -u '+%Y%m%d')"

# Create a placeholder git commit value if the git env cannot be found
null_git_commit="no-git-env"
git_commit="$(git describe --tags --always --dirty)" || \
    git_commit="$null_git_commit"

image_tag="v${build_date}-${git_commit}"
if [[ ${git_commit} == "$null_git_commit" ]]; then
    image_tag="${GIT_TAG}"
fi

p_ STABLE_GIT_COMMIT "${git_commit}"
p_ STABLE_IMG_REGISTRY gcr.io
p_ STABLE_IMG_REPOSITORY k8s-staging-artifact-promoter
p_ STABLE_IMG_NAME cip
p_ STABLE_IMG_TAG "${image_tag}"
p_ STABLE_TEST_AUDIT_PROD_IMG_REPOSITORY us.gcr.io/k8s-gcr-audit-test-prod
p_ STABLE_TEST_AUDIT_STAGING_IMG_REPOSITORY gcr.io/k8s-gcr-audit-test-prod
p_ STABLE_TEST_AUDIT_PROJECT_ID k8s-gcr-audit-test-prod
p_ STABLE_TEST_AUDIT_PROJECT_NUMBER 375340694213
p_ STABLE_TEST_AUDIT_INVOKER_SERVICE_ACCOUNT k8s-infra-gcr-promoter@k8s-gcr-audit-test-prod.iam.gserviceaccount.com
p_ STABLE_TEST_STAGING_IMG_REPOSITORY gcr.io/k8s-staging-cip-test
