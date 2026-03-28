# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o media-processor ./cmd/server

# Production stage
FROM alpine:latest

RUN apk add --no-cache ffmpeg libwebp-tools ca-certificates

RUN adduser -D -g '' appuser

WORKDIR /app
COPY --from=builder /app/media-processor .

RUN chown -R appuser:appuser /app
USER appuser

EXPOSE 4000

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -q --spider http://localhost:4000/health || exit 1

CMD ["./media-processor"]
