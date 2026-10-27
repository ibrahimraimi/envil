FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-w -s" -o /bin/envil-server ./cmd/envil-server

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 10001 -S envil && \
    adduser -u 10001 -S envil -G envil

USER envil:envil

COPY --from=builder /bin/envil-server /usr/local/bin/envil-server

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/envil-server"]
