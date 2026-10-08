FROM ghcr.io/formancehq/base:scratch
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/operator-utils /usr/bin/operator-utils
ENV OTEL_SERVICE_NAME=operator-utils
ENTRYPOINT ["/usr/bin/operator-utils"]
