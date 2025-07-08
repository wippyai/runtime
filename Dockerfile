# ---- Go Builder Stage ----
FROM golang:1.24 AS go-build

RUN apt-get update && apt-get install -y \
    gcc \
    libc6-dev \
    libsqlite3-dev \
    git \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*
# Enable SSH for private repo access
ENV GOPRIVATE=github.com/wippyai/*

RUN git config --global url."git@github.com:".insteadOf "https://github.com/"
RUN mkdir -p ~/.ssh && ssh-keyscan github.com >> ~/.ssh/known_hosts

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=ssh go mod download

COPY . .
# Build the Go application for Linux, outputting to 'server'
RUN CGO_ENABLED=1 \
    GOOS=linux \
    GOARCH=amd64 \
    CGO_CPPFLAGS="-I/usr/local/opt/sqlite/include" \
    CGO_LDFLAGS="-L/usr/local/opt/sqlite/lib" \
    go build --tags "fts5 sqlite_vec" -o server ./cmd/runner

# ---- Final Stage ----
FROM gcr.io/distroless/base:nonroot
WORKDIR /app

# Copy only the compiled binary from the Go builder stage
COPY --from=go-build /app/server .

# Define the entrypoint
ENTRYPOINT ["/app/server"]
