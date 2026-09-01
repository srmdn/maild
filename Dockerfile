FROM golang:1.27-alpine AS build
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags "-X github.com/srmdn/maild/internal/buildinfo.Version=${VERSION}" -o /out/maild ./cmd/server

FROM alpine:3.24
RUN adduser -D -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /out/maild /app/maild
EXPOSE 8080
ENTRYPOINT ["/app/maild"]

