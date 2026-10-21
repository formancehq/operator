FROM ghcr.io/formancehq/base:26.04
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/operator /usr/bin/operator
ENV OTEL_SERVICE_NAME operator
ENTRYPOINT ["/usr/bin/operator"]
