FROM golang:1.13

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o descaler


FROM debian:buster

COPY --from=0 /go/src/app/descaler /usr/local/bin/descaler
CMD ["descaler"]
