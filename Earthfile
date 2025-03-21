VERSION 0.8

IMPORT github.com/formancehq/earthly:tags/v0.19.1 AS core

FROM core+base-image

sources:
    FROM core+builder-image
    WORKDIR /src
    WORKDIR /src/components/operator
    COPY --dir api internal pkg cmd config .
    COPY go.* .
    SAVE ARTIFACT /src

compile:
    FROM core+builder-image
    COPY (+sources/*) /src
    COPY --pass-args (+generate/*) /src/components/operator
    WORKDIR /src/components/operator/cmd
	DO --pass-args core+GO_COMPILE

build-image:
    FROM core+final-image
    ENTRYPOINT ["/usr/bin/operator"]
    COPY --pass-args (+compile/main) /usr/bin/operator
    ARG REPOSITORY=ghcr.io
    ARG tag=latest
    DO --pass-args core+SAVE_IMAGE --COMPONENT=operator --TAG=$tag

controller-gen:
    FROM core+builder-image
    DO --pass-args core+GO_INSTALL --package=sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0

manifests:
    FROM --pass-args +controller-gen
    COPY (+sources/*) /src
    WORKDIR /src/components/operator
    COPY --dir config .
    RUN --mount=type=cache,id=gomod,target=${GOPATH}/pkg/mod \
        --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
        controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

    SAVE ARTIFACT config AS LOCAL config

generate:
    FROM --pass-args +controller-gen
    COPY +sources/* /src
    WORKDIR /src/components/operator
    COPY --dir hack .
    RUN --mount=type=cache,id=gomod,target=${GOPATH}/pkg/mod \
        --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
        controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
    SAVE ARTIFACT internal AS LOCAL internal
    SAVE ARTIFACT api AS LOCAL api

generate-mock:
    FROM core+builder-image
    DO --pass-args core+GO_INSTALL --package=go.uber.org/mock/mockgen@latest
    COPY (+sources/*) /src
    WORKDIR /src/components/operator
    RUN go generate -run mockgen ./...
    SAVE ARTIFACT internal AS LOCAL internal

helm-update:
    FROM core+builder-image
    RUN curl -s https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh | bash -s -- 4.5.4 /bin
    RUN apk add helm yq

    WORKDIR /src
    COPY --pass-args (+manifests/config) /src/config
    COPY --dir helm hack .

    RUN rm -f helm/operator/templates/gen/*
    RUN rm -f helm/crds/templates/crds/*

    RUN kustomize build config/default --output helm/operator/templates/gen
    RUN kustomize build config/crd --output helm/crds/templates/crds

    RUN rm -f helm/operator/templates/gen/v1_namespace*.yaml
    RUN rm -f helm/operator/templates/gen/apps_v1_deployment_*.yaml
    RUN helm dependencies update ./helm/operator


    SAVE ARTIFACT helm AS LOCAL helm

deploy:
    ARG LICENCE_TOKEN=""
    ARG LICENCE_ISSUER="http://licence-api.formance.svc.cluster.local:8080/keys"

    COPY (+sources/*) /src
    LET tag=$(tar cf - /src | sha1sum | awk '{print $1}')
    WAIT
        BUILD --pass-args +build-image --tag=$tag
        BUILD --pass-args ./tools/utils+build-image --tag=$tag
    END
    FROM --pass-args core+vcluster-deployer-image
    COPY --pass-args (+helm-update/helm) helm
    WORKDIR helm

    # When all clients are using the new base CRDS chart, we can uncomment this step
    # RUN helm upgrade --install --namespace formance-system --install formance-operator-crds \
    #     --wait \
    #     --create-namespace ./base
    RUN --no-cache helm upgrade --install --namespace formance-system --install formance-operator \
        --wait \
        --create-namespace \
        --set image.tag=$tag \
        --set operator.licence.token=$LICENCE_TOKEN \
        --set operator.licence.issuer=$LICENCE_ISSUER ./operator \
        --set operator.dev=true
    WORKDIR /
    COPY .earthly .earthly
    WORKDIR .earthly
    RUN kubectl get versions default || kubectl apply -f k8s-versions.yaml
    ARG user
    RUN --secret tld helm upgrade --install operator-configuration ./configuration \
        --namespace formance-system \
        --set gateway.fallback=https://console.$user.$tld

deploy-staging:
    FROM --pass-args core+base-argocd 
    ARG --required TAG
    ARG APPLICATION=staging-eu-west-1-hosting-operator
    LET SERVER=argocd.internal.formance.cloud
    RUN --secret AUTH_TOKEN \
        argocd app set $APPLICATION \ 
        --parameter image.tag=$TAG \
        --auth-token=$AUTH_TOKEN --server=$SERVER --grpc-web
    RUN --secret AUTH_TOKEN argocd --auth-token=$AUTH_TOKEN --server=$SERVER --grpc-web app sync $APPLICATION


lint:
    FROM +tidy
    WORKDIR /src/components/operator
    DO --pass-args core+GO_LINT
    SAVE ARTIFACT api AS LOCAL api
    SAVE ARTIFACT internal AS LOCAL internal

tests:
    FROM core+builder-image
    RUN apk update && apk add bash
    DO --pass-args core+GO_INSTALL --package=sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20240320141353-395cfc7486e6
    ENV ENVTEST_VERSION 1.28.0
    RUN setup-envtest use $ENVTEST_VERSION -p path
    ENV KUBEBUILDER_ASSETS /root/.local/share/kubebuilder-envtest/k8s/$ENVTEST_VERSION-linux-$(go env GOHOSTARCH)
    DO --pass-args core+GO_INSTALL --package=github.com/onsi/ginkgo/v2/ginkgo@v2.14.0
    COPY (+sources/*) /src
    COPY --pass-args (+manifests/config) /src/components/operator/config
    COPY --pass-args (+generate/internal) /src/components/operator/internal
    COPY --pass-args (+generate/api) /src/components/operator/api
    WORKDIR /src/components/operator
    COPY --dir hack .
    ARG GOPROXY
    ARG updateTestData=0
    ENV UPDATE_TEST_DATA=$updateTestData
    ARG args=
    RUN --mount=type=cache,id=gomod,target=$GOPATH/pkg/mod \
        --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
        ginkgo $args -p ./...
    IF [ "$updateTestData" = "1" ]
        SAVE ARTIFACT internal/tests/testdata AS LOCAL internal/tests/testdata
    END

generate-docs:
    FROM core+builder-image
    COPY (+sources/*) /src
    RUN go install github.com/elastic/crd-ref-docs@v0.0.12
    WORKDIR /src/components/operator
    COPY docs.config.yaml .
    COPY --dir api crd-doc-templates .
    COPY docs docs
    RUN mkdir -p "docs/09-Configuration reference"
    RUN crd-ref-docs \
        --source-path=api/formance.com/v1beta1 \
        --renderer=markdown \
        --output-path="./docs/09-Configuration reference/02-Custom Resource Definitions.md" \
        --templates-dir=./crd-doc-templates \
        --config=./docs.config.yaml
    SAVE ARTIFACT docs/* AS LOCAL docs/

pre-commit:
    WAIT
        BUILD --pass-args +tidy
    END
    BUILD --pass-args +lint
    BUILD --pass-args +generate
    BUILD --pass-args +manifests
    BUILD --pass-args +helm-update
    BUILD --pass-args +helm-validate
    BUILD +generate-docs

openapi:
    RUN echo "not implemented"

tidy:
    FROM +sources
    WORKDIR /src/components/operator
    DO --pass-args core+GO_TIDY
    # BUILD ./tools/utils+tidy
    # BUILD ./tools/kubectl-stacks+tidy

helm-validate:
    FROM --pass-args core+helm-base
    WORKDIR /src
    COPY --pass-args (+helm-update/helm) .
    
    FOR dir IN $(ls -d */)
        WORKDIR /src/$dir
        DO --pass-args core+HELM_VALIDATE
    END
    SAVE ARTIFACT /src AS LOCAL helm

helm-package:
    FROM --pass-args +helm-validate
    WORKDIR /src
    FOR dir IN $(ls -d */)
        WORKDIR /src/$dir
        RUN helm package .
    END
    SAVE ARTIFACT /src AS LOCAL helm

helm-publish:
    FROM --pass-args +helm-package
    WORKDIR /src
    FOR path IN $(ls -d ls */*.tgz)
        DO --pass-args +HELM_PUBLISH --path=/src/${path}
    END

release:
    FROM core+builder-image
    ARG mode=local
    COPY --dir . /src
    WAIT
      DO core+GORELEASER --mode=$mode
    END
    BUILD +helm-publish 

HELM_PUBLISH:
    FUNCTION
    ARG --required path
    WITH DOCKER
        RUN --secret GITHUB_TOKEN echo $GITHUB_TOKEN | docker login ghcr.io -u NumaryBot --password-stdin
    END
    WITH DOCKER
        RUN helm push ${path} oci://ghcr.io/formancehq/helm
    END