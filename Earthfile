VERSION 0.8

ARG core=github.com/formancehq/earthly:main
IMPORT $core AS core

FROM core+base-image

sources:
    FROM core+builder-image
    WORKDIR /src
    WORKDIR /src/components/operator
    COPY --dir api internal pkg cmd config .
    COPY go.* .
    SAVE ARTIFACT /src

controller-gen:
    FROM core+builder-image
    DO --pass-args core+GO_INSTALL --package=sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0

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
    COPY --dir helm helm
    WORKDIR helm

    # When all clients are using the new base CRDS chart, we can uncomment this step
    # RUN helm upgrade --install --namespace formance-system --install formance-operator-crds \
    #     --wait \
    #     --create-namespace ./base
    RUN helm dependency update ./operator

    ARG FORMANCE_DEV_CLUSTER_V2=no
    LET ADDITIONAL_ARGS=""
    IF [ "$FORMANCE_DEV_CLUSTER_V2" == "yes" ]
        SET ADDITIONAL_ARGS="$ADDITIONAL_ARGS --set imagePullSecrets[0].name=zot"
        ARG --required REPOSITORY=
        SET ADDITIONAL_ARGS="$ADDITIONAL_ARGS --set image.repository=$REPOSITORY/formancehq/operator"
    END

    RUN --no-cache helm upgrade --install --namespace formance-system --install formance-operator \
        --wait \
        --debug \
        --create-namespace \
        --set image.tag=$tag \
        --set operator.licence.token=$LICENCE_TOKEN \
        --set operator.licence.issuer=$LICENCE_ISSUER ./operator \
        --set operator.utils.tag=$tag \
        --set operator.dev=true $ADDITIONAL_ARGS
    WORKDIR /
    COPY .earthly .earthly
    WORKDIR .earthly
    RUN kubectl get versions default || kubectl apply -f k8s-versions.yaml
    ARG user

    SET ADDITIONAL_ARGS=""
    IF [ "$FORMANCE_DEV_CLUSTER_V2" == "yes" ]
        SET ADDITIONAL_ARGS="$ADDITIONAL_ARGS --set v2=true"
        SET ADDITIONAL_ARGS="$ADDITIONAL_ARGS --set ghcrRegistry=$REPOSITORY"
    END
    RUN --secret tld helm upgrade --install operator-configuration ./configuration \
        --namespace formance-system \
        --set gateway.fallback=https://console.$user.$tld $ADDITIONAL_ARGS

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

### Following targets are used by agent tests
manifests:
    FROM --pass-args +controller-gen
    COPY (+sources/*) /src
    WORKDIR /src/components/operator
    COPY --dir config .
    RUN --mount=type=cache,id=gomod,target=${GOPATH}/pkg/mod \
        --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
        controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

    SAVE ARTIFACT config AS LOCAL config