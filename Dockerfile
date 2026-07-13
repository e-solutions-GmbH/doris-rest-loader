# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.24 AS builder

WORKDIR /app

# Download dependencies first so this layer is cached independently of source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a fully static binary.
# -trimpath   removes local file paths from the binary (reproducible + privacy).
# -ldflags    strips debug symbols and DWARF info to minimise image size.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /doris-rest-loader \
      ./cmd/doris-rest-loader

# ── Runtime stage ─────────────────────────────────────────────────────────────
# distroless/static contains no shell, package manager, or libc.
# It is the recommended minimal base for statically compiled Go binaries.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /doris-rest-loader /doris-rest-loader

# Run as the built-in non-root user provided by the distroless image.
USER nonroot:nonroot

ENTRYPOINT ["/doris-rest-loader"]

# Default config path; override with:
#   docker run ... -v /host/config.yaml:/etc/doris-rest-loader/config.yaml
# or pass --config /custom/path.yaml as a CMD argument.
CMD ["--config", "/etc/doris-rest-loader/config.yaml"]
