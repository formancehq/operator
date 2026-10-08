FROM ghcr.io/formancehq/base:22.04
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/operator-utils /usr/bin/operator-utils
ENV OTEL_SERVICE_NAME=operator-utils
ENTRYPOINT ["/usr/bin/operator-utils"]
