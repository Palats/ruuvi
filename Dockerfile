FROM golang:1.14 AS builder

WORKDIR /go/src/app
COPY *.go ./
COPY go.* ./

RUN go get -d -v ./...
RUN CGO_ENABLED=0 GOOS=linux go build server.go


FROM alpine:latest AS run
WORKDIR /app
COPY --from=builder /go/src/app/server .

CMD ["./server"]