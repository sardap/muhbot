FROM golang:latest

RUN apt-get update && apt-get -y install libopus-dev libopusfile-dev

WORKDIR /app
COPY go.mod .
COPY go.sum .
COPY main.go .
RUN go build -o main .

CMD ["/app/main"]