# Build stage
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/readitall ./cmd/readitall

# Runtime: chromedp/headless-shell is distroless-based and ships only
# headless Chrome at /headless-shell/headless-shell (on PATH, where
# chromedp's exec allocator finds it).
FROM chromedp/headless-shell:latest
LABEL org.opencontainers.image.source="https://github.com/lukaszraczylo/mcp-readitall"
COPY --from=build /out/readitall /usr/local/bin/readitall
ENV READITALL_SESSIONS_DIR=/data \
    READITALL_ADDR=:8080
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/readitall"]
