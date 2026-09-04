# syntax=docker/dockerfile:1.7

FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/synfactory ./cmd/synfactory

FROM alpine:3.22 AS control
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates curl tini \
    && addgroup -S -g 10001 synfactory \
    && adduser -S -D -u 10001 -G synfactory -h /home/synfactory synfactory \
    && mkdir -p /var/lib/synfactory/repos /var/lib/synfactory/workspaces /etc/synfactory \
    && chown -R synfactory:synfactory /var/lib/synfactory /etc/synfactory
COPY --from=build /out/synfactory /usr/local/bin/synfactory
USER synfactory
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/synfactory"]
CMD ["api"]

FROM control AS worker
USER root
RUN apk add --no-cache git github-cli openssh-client postgresql-client docker-cli nodejs npm
USER synfactory
CMD ["worker"]
