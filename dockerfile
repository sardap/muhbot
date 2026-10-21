FROM golang:latest AS builder

WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN go build -o main .

# Backend
FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/main main

ENTRYPOINT [ "/app/main" ]
