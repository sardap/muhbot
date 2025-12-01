FROM golang:tip-alpine3.22 as builder

WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN go build -o main .

# Backend
FROM alpine:3.22.2

WORKDIR /app
COPY --from=builder /app/main main

ENTRYPOINT [ "/app/main" ]
