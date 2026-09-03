# syntax=docker/dockerfile:1.7

FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/synfactory ./cmd/synfactory

FROM debian:bookworm-slim AS control
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tini \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --home-dir /home/synfactory synfactory \
    && mkdir -p /var/lib/synfactory/repos /var/lib/synfactory/workspaces /etc/synfactory \
    && chown -R synfactory:synfactory /var/lib/synfactory /etc/synfactory
COPY --from=build /out/synfactory /usr/local/bin/synfactory
USER synfactory
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/synfactory"]
CMD ["api"]

FROM control AS worker
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends git gh openssh-client postgresql-client docker.io nodejs npm \
    && rm -rf /var/lib/apt/lists/*
USER synfactory
CMD ["worker"]
