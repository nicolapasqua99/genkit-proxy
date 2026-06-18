# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.25 AS build
WORKDIR /src

# Download modules first so this layer caches unless go.mod/go.sum change.
COPY go.* ./
RUN go mod download

# Build a small static binary. Point this at your binary's package (e.g. ./cmd/app).
# Pass VCS_REF (git SHA) and BUILD_TIME at build time to embed version info.
COPY . .
ARG VCS_REF=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X main.version=${VCS_REF} -X main.buildTime=${BUILD_TIME}" \
    -o /out/server ./cmd/app

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
# Cloud Run injects PORT (default 8080); the server must listen on $PORT.
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
