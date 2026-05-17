# Stage 1: Build Go kono binary
FROM golang:1.26.3-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION

RUN make all GOOS=${TARGETOS} GOARCH=${TARGETARCH} VERSION=${VERSION}

# Stage 2: Final minimal image
FROM chainguard/wolfi-base@sha256:0cff4df29a6597173dc8b813787318150141eb96ac783dc3ff4f5ff52c49a1e2 AS runtime

COPY --from=builder /app/.bin/kono /usr/local/bin/kono

COPY --from=builder /app/build/plugins /usr/local/lib/kono/plugins
COPY --from=builder /app/build/middlewares /usr/local/lib/kono/middlewares

RUN addgroup -S -g 1000 kono \
    && adduser -S -u 1000 -G kono -s /sbin/nologin -H kono

EXPOSE 7805/tcp

STOPSIGNAL SIGTERM

USER kono:kono

ENTRYPOINT ["kono"]
CMD ["serve"]