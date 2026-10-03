# Stage 1: Build Go aastro binary
FROM golang:1.26.7-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG TARGETOS
ARG TARGETARCH

ARG VERSION
ARG COMMIT
ARG BUILD_DATE

RUN make build plugins \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} VERSION=${VERSION} COMMIT=${COMMIT} BUILD_DATE=${BUILD_DATE}

# Stage 2: Final minimal image
FROM chainguard/wolfi-base@sha256:a68cc72c5bb76e13b162fc0c70da5aca179167d948d4f49a0dbdf2b74009665d AS runtime

ARG VERSION
ARG COMMIT

LABEL org.opencontainers.image.title="aastro" \
      org.opencontainers.image.description="Aastro API Gateway" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/voidrunner3074/aastro" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=builder /app/.bin/aastro /usr/local/bin/aastro
COPY --from=builder /app/.bin/aastroctl /usr/local/bin/aastroctl

COPY --from=builder /app/build/plugins /usr/local/lib/aastro/plugins
COPY --from=builder /app/build/middlewares /usr/local/lib/aastro/middlewares

RUN addgroup -S -g 1000 aastro \
    && adduser -S -u 1000 -G aastro -s /sbin/nologin -H aastro

EXPOSE 7805/tcp

STOPSIGNAL SIGTERM

USER aastro:aastro

ENTRYPOINT ["aastro"]