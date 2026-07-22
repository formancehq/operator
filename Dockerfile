FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=latest
ARG COMMIT=unknown
ARG LICENCE_PUBLIC_KEY_B64=""

WORKDIR /src/cmd

RUN set -e; \
    LDFLAGS="-X github.com/formancehq/operator/v3/cmd.Version=${VERSION} \
    -X github.com/formancehq/operator/v3/cmd.BuildDate=$(date +%s) \
    -X github.com/formancehq/operator/v3/cmd.Commit=${COMMIT}"; \
    if [ -n "$LICENCE_PUBLIC_KEY_B64" ]; then \
        LICENCE_PUBLIC_KEY="$(printf '%s' "$LICENCE_PUBLIC_KEY_B64" | base64 -d)"; \
        LDFLAGS="${LDFLAGS} -X 'github.com/formancehq/go-libs/v5/pkg/authn/licence.formancePublicKey=${LICENCE_PUBLIC_KEY}'"; \
    fi; \
    CGO_ENABLED=0 go build -buildvcs=false -o /usr/bin/operator -ldflags="${LDFLAGS}" .

FROM alpine:3.20

RUN apk update && apk add --no-cache ca-certificates curl
RUN addgroup -S operator && adduser -S -G operator operator

ENTRYPOINT ["/usr/bin/operator"]

COPY --from=builder /usr/bin/operator /usr/bin/operator

USER operator
