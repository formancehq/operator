set dotenv-load
set positional-arguments

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

release-ci: helm-publish
  goreleaser release --nightly --clean

release: helm-publish
  goreleaser release --clean

manifests:
  go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.18.0 \
    rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate:
  go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.18.0 \
    object:headerFile="hack/boilerplate.go.txt" paths="./..."

generate-mock:
  go generate -run mockgen ./...

helm-update:
  #!/bin/bash
  set pipefail -e

  rm -f helm/operator/templates/gen/*
  rm -f helm/crds/templates/crds/*

  kustomize build config/default --output helm/operator/templates/gen
  kustomize build config/crd --output helm/crds/templates/crds

  # Patch all CRD files to add helm.sh/resource-policy and custom annotations support
  for file in helm/crds/templates/crds/*.yaml; do
    awk '/controller-gen.kubebuilder.io\/version:/ {
      print
      print "    helm.sh/resource-policy: keep"
      print "    {{{{- with .Values.annotations }}"
      print "    {{{{- toYaml . | nindent 4 }}"
      print "    {{{{- end }}"
      next
    } 1' "$file" > "$file.tmp" && mv "$file.tmp" "$file"
  done

  rm -f helm/operator/templates/gen/v1_namespace*.yaml
  rm -f helm/operator/templates/gen/apps_v1_deployment_*.yaml
  helm dependencies update ./helm/operator

helm-validate args='':
  for dir in $(ls -d helm/*); do \
    helm lint ./$dir --strict {{args}}; \
    helm template ./$dir {{args}}; \
  done

helm-package: helm-update
  for dir in $(ls -d helm/*); do \
    pushd $dir && helm package . && popd; \
  done

helm-publish: helm-package
  echo $GITHUB_TOKEN | docker login ghcr.io -u NumaryBot --password-stdin
  for path in $(ls -d helm/*/*.tgz); do \
    helm push ${path} oci://ghcr.io/formancehq/helm; \
  done

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
