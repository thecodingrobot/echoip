# Build stage
FROM golang:1.26-trixie AS build
WORKDIR /go/src/github.com/mpolden/echoip

# Must build without cgo because libc is unavailable in runtime image
ENV CGO_ENABLED=0

# Cache module downloads independently of source changes
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make

# Runtime stage
FROM gcr.io/distroless/static-debian13:nonroot
EXPOSE 8080

COPY --from=build /go/bin/echoip /opt/echoip/
COPY html /opt/echoip/html

WORKDIR /opt/echoip
USER nonroot:nonroot
ENTRYPOINT ["/opt/echoip/echoip"]
