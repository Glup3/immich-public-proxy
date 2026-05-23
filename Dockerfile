FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /immich-public-proxy ./cmd/immich-public-proxy

FROM alpine:3.22 AS runner

RUN apk --no-cache add curl \
    && addgroup -S app \
    && adduser -S app -G app

WORKDIR /app
COPY --from=builder /immich-public-proxy /usr/local/bin/immich-public-proxy
COPY templates/ ./templates/
COPY public/ ./public/
COPY config.json ./config.json

ARG PACKAGE_VERSION
ENV APP_VERSION=${PACKAGE_VERSION}

USER app
EXPOSE 3000

CMD ["immich-public-proxy"]
