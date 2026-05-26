# Stage 1: Build Go aastro binary
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

COPY --from=builder /app/.bin/aastro /usr/local/bin/aastro

COPY --from=builder /app/build/plugins /usr/local/lib/aastro/plugins
COPY --from=builder /app/build/middlewares /usr/local/lib/aastro/middlewares

RUN addgroup -S -g 1000 aastro \
    && adduser -S -u 1000 -G aastro -s /sbin/nologin -H aastro

EXPOSE 7805/tcp

STOPSIGNAL SIGTERM

USER aastro:aastro

ENTRYPOINT ["aastro"]
CMD ["serve"]