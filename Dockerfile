FROM golang:1.26-alpine AS gobuild
ARG CMD=api
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/ephematool/ephematool/internal/buildinfo.Version=${VERSION} -X github.com/ephematool/ephematool/internal/buildinfo.Commit=${COMMIT} -X github.com/ephematool/ephematool/internal/buildinfo.BuiltAt=${BUILT_AT}" \
    -o /out/app ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12:nonroot AS go-runtime
COPY --from=gobuild /out/app /app
USER nonroot
ENTRYPOINT ["/app"]
CMD ["serve"]
