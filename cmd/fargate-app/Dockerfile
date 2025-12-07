# Multi-stage build for Fargate
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app-binary ./cmd/fargate-app/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app-binary /app-binary
ENTRYPOINT ["/app-binary"]
