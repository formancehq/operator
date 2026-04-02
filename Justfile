set dotenv-load
set positional-arguments

import '.just-lib/helm/recipes.just'
export HELM_DIR := "helm"

default:
  @just --list

pre-commit: tidy lint generate manifests helm-update helm-validate generate-docs
pc: pre-commit

lint:
  golangci-lint run --fix --timeout 5m
  cd ./tools/kubectl-stacks && golangci-lint run --fix --timeout 5m
  cd ./tools/utils && golangci-lint run --fix --timeout 5m

tidy:
  go mod tidy
  cd ./tools/kubectl-stacks && go mod tidy
  cd ./tools/utils && go mod tidy

tests args='':
  KUBEBUILDER_ASSETS=$(setup-envtest use 1.32.0 -p path) ginkgo -p ./...

release-local:
  goreleaser release --nightly --skip=publish --clean

release-ci: (helm-publish `git rev-parse --short HEAD`)
  goreleaser release --nightly --clean

release: (helm-publish)
  goreleaser release --clean

manifests:
  go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.18.0 \
    rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate:
  go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.18.0 \
    object:headerFile="hack/boilerplate.go.txt" paths="./..."

generate-mock:
  go generate -run mockgen ./...

generate-docs:
  mkdir -p "docs/09-Configuration reference"
  go run github.com/elastic/crd-ref-docs@v0.2.0 \
    --source-path=api/formance.com/v1beta1 \
    --renderer=markdown \
    --output-path="./docs/09-Configuration reference/02-Custom Resource Definitions.md" \
    --templates-dir=./crd-doc-templates \
    --config=./docs.config.yaml

deploy: helm-update
  earthly +deploy
