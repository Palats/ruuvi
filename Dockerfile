FROM golang:1.21 AS builder

RUN groupadd server && useradd --no-log-init --gid server --create-home server
USER server:server

RUN mkdir /home/server/src

WORKDIR /home/server/src/
COPY *.go ./
COPY --chown=server:server go.* ./
RUN ls -al /home

RUN go get -d -v ./...
RUN CGO_ENABLED=0 GOOS=linux go build server.go


FROM alpine:latest AS run
RUN addgroup -S server && adduser -S server -G server
USER server:server
WORKDIR /home/server
COPY --from=builder /home/server/src/server .

CMD ["./server"]