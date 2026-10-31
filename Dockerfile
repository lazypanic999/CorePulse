FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/corepulse ./cmd/corepulse

FROM registry.k8s.io/kubectl:v1.35.1
LABEL org.opencontainers.image.source="https://github.com/lazypanic999/CorePulse" \
      org.opencontainers.image.title="CorePulse" \
      org.opencontainers.image.description="5G core lab observability and guarded fault experiments" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/corepulse /usr/local/bin/corepulse
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/corepulse"]
