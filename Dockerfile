# Build
FROM golang:1.23-bookworm AS build
WORKDIR /go/src/github.com/mpolden/echoip
COPY . .

# Must build without cgo because libc is unavailable in runtime image
ENV GO111MODULE=on CGO_ENABLED=0
RUN make

# Run
FROM scratch
EXPOSE 8080

COPY --from=build /go/bin/echoip /opt/echoip/
COPY html /opt/echoip/html
COPY data /opt/echoip/data

WORKDIR /opt/echoip
ENTRYPOINT ["/opt/echoip/echoip"]
