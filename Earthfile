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