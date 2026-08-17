# syntax=docker/dockerfile:1

FROM golang:1.26 AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/microgrid-ops ./cmd/server

FROM golang:1.26 AS certs
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

FROM scratch
COPY --from=builder /out/microgrid-ops /microgrid-ops
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY config.json /config.json
WORKDIR /tmp
ENV STORE_PATH=/tmp/data/state.json
EXPOSE 58295
ENTRYPOINT ["/microgrid-ops", "-config", "/config.json"]
