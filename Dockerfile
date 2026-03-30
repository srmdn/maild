FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/maild ./cmd/server

FROM alpine:3.22
RUN adduser -D -u 10001 appuser
USER appuser
WORKDIR /app
COPY --from=build /out/maild /app/maild
EXPOSE 8080
ENTRYPOINT ["/app/maild"]

