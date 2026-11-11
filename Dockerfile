FROM node:25.9.0-bookworm-slim AS web
WORKDIR /build
RUN npm install --global pnpm@10.16.0
COPY web/package.json web/pnpm-lock.yaml ./web/
RUN pnpm --dir web install --frozen-lockfile
COPY web ./web
COPY internal/buddy/corpus.json ./internal/buddy/corpus.json
RUN pnpm --dir web build

FROM golang:1.25.5-alpine AS go
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -o /out/corporate ./cmd/corporate

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates && addgroup -S buddy && adduser -S -G buddy -u 10001 buddy
WORKDIR /app
COPY --from=go /out/server /out/corporate /app/
USER buddy

FROM runtime AS corporate
EXPOSE 8091
ENTRYPOINT ["/app/corporate"]

FROM runtime AS app
COPY --from=web /build/web/dist ./web/dist
EXPOSE 8090
ENTRYPOINT ["/app/server"]
