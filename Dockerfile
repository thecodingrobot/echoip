# Build stage
FROM golang:1.26-trixie AS build
WORKDIR /go/src/github.com/mpolden/echoip
COPY . .

# Must build without cgo because libc is unavailable in runtime image
ENV GO111MODULE=on CGO_ENABLED=0
RUN make

# Runtime stage
FROM gcr.io/distroless/static-debian13
EXPOSE 8080

COPY --from=build /go/bin/echoip /opt/echoip/
COPY html /opt/echoip/html

WORKDIR /opt/echoip
ENTRYPOINT ["/opt/echoip/echoip"]
