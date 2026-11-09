# Both build stages run on the native build platform and only the Go binary is
# cross-compiled, so multi-arch images build without QEMU emulation.
FROM --platform=$BUILDPLATFORM node:26-alpine AS frontend

WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY index.html svelte.config.js tsconfig*.json vite.config.ts ./
COPY public ./public
COPY src ./src
RUN npm run check && npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS backend

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY config ./config
COPY internal ./internal
RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/mydashboard ./cmd/mydashboard

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
LABEL org.opencontainers.image.title="myDashboard" \
      org.opencontainers.image.description="A calm operational dashboard for Docker Compose homelabs" \
      org.opencontainers.image.source="https://github.com/patbaumgartner/my-dashboard" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"

WORKDIR /app
COPY --from=backend --chown=65532:65532 /out/mydashboard /app/mydashboard
COPY --from=frontend --chown=65532:65532 /src/dist /app/public
# An example manifest so the image starts out of the box. Mount your own over it.
COPY --chown=65532:65532 config/services.example.json /app/config/services.json

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/mydashboard", "healthcheck"]

ENTRYPOINT ["/app/mydashboard"]
