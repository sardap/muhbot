FROM golang:1.16.7-alpine3.13 as builder

WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY main.go .
RUN go build -o main .

# Backend
FROM alpine:3.13.6

WORKDIR /app
COPY --from=builder /app/main main

ENTRYPOINT [ "/app/main" ]
