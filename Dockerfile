# Build the manager binary
FROM golang:1.18 as builder
WORKDIR /workspace
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -v -a -o manager main.go

FROM gcr.io/distroless/static:nonroot as release
LABEL org.opencontainers.image.source https://github.com/formancehq/operator
WORKDIR /
COPY manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
LABEL org.opencontainers.image.source https://github.com/formancehq/operator
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532
ENTRYPOINT ["/manager"]
