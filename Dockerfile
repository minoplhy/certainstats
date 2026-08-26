# Stage 1: Go Binary Build (Runs natively on build host, cross-compiles to target)
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
WORKDIR /app

# Docker automatic platform args
ARG TARGETOS
ARG TARGETARCH

# Copy module files and download dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy application source files (including web/ templates and static assets)
COPY . .

# Build the self-contained Go binary cross-compiling to target platform
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embed -ldflags="-w -s" -o certainstats ./cmd/certainstats

# Stage 2: Final Production Runtime Image
FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata

# Copy executable from builder stage
COPY --from=backend-builder /app/certainstats .

# Create directory for persistent database/TSDB data
RUN mkdir -p /app/data

# Default production environment variables
ENV PANEL_PATH="/"
ENV PUBLIC_PATH="/dashboard"
ENV DATA_DIR="/app/data"

EXPOSE 8080
CMD ["./certainstats"]
