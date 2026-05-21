# Stage 1a: Admin Frontend Build (Runs natively on build host)
FROM --platform=$BUILDPLATFORM node:22-alpine AS admin-builder
WORKDIR /app/frontend-admin
COPY frontend-admin/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm install
COPY frontend-admin/ ./
RUN npm run build

# Stage 1b: Public Frontend Build (Runs natively on build host)
FROM --platform=$BUILDPLATFORM node:22-alpine AS public-builder
WORKDIR /app/frontend-public
COPY frontend-public/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm install
COPY frontend-public/ ./
RUN npm run build

# Stage 2: Backend Build (Runs natively on build host, cross-compiles to target)
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
WORKDIR /app

# Docker automatic platform args
ARG TARGETOS
ARG TARGETARCH

# Copy module files and download dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy backend source files
COPY . .

# Copy built frontend assets into the Go build context so that they can be embedded
COPY --from=admin-builder /app/frontend-admin/out ./frontend-admin/out
COPY --from=public-builder /app/frontend-public/out ./frontend-public/out

# Build the self-contained Go binary cross-compiling to the target platform
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags embed -ldflags="-w -s" -o certainstats ./cmd/certainstats

# Stage 3: Final Production Runtime Image
FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata

# Copy only the self-contained executable from the backend builder stage
COPY --from=backend-builder /app/certainstats .

# Create directory for persistent database/TSDB data
RUN mkdir -p /app/data

# Default production environment variables
ENV PANEL_PATH="/"
ENV PUBLIC_PATH="/dashboard"
ENV DATA_DIR="/app/data"
ENV ALLOWED_ORIGINS="http://localhost:8080/"

EXPOSE 8080
CMD ["./certainstats"]
