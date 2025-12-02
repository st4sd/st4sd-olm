# Build the manager binary
FROM quay.io/projectquay/golang:1.25 as builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.0-dev

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY deploy/ deploy/
COPY controllers/ controllers/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags="-X 'github.com/st4sd/st4sd-olm/api/v1alpha1.OPERATOR_VERSION=$VERSION'" -a -o manager main.go

RUN echo 'You can find the licenses of GPL packages in this container under \n\
/usr/share/doc/${PACKAGE_NAME}/copyright \n\
\n\
If you would like the source to the GPL packages in this image then \n\
send a request to this address, specifying the package you want and \n\
the name and hash of this image: \n\
\n\
IBM Research Ireland,\n\
IBM Technology Campus\n\
Damastown Industrial Park\n\
Mulhuddart Co. Dublin D15 HN66\n\
Ireland\n' >/gpl-licenses


# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .

COPY --from=builder /gpl-licenses /gpl-licenses
COPY st4sd-deployment/helm-chart/ helm-chart/


USER 65532:65532


ENTRYPOINT ["/manager"]
