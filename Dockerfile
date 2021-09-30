FROM golang:1.16-alpine as builder
WORKDIR /src


ENV CMD_PREFIX=!
ENV ROOM_MATCH="chess-.*"

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN CGO_ENABLE=0 go build ./cmd/discordchess

# Build stockfish in alpine
FROM alpine:3 as stockfish

ENV SOURCE_REPO https://github.com/official-stockfish/Stockfish
ENV VERSION master

ADD ${SOURCE_REPO}/archive/${VERSION}.tar.gz /root
WORKDIR /root

RUN if [ ! -d Stockfish-${VERSION} ]; then tar xvzf *.tar.gz; fi \
  && cd Stockfish-${VERSION}/src \
  && apk add make g++ \
  && make build ARCH=x86-64-modern LDFLAGS="-static -static-libstdc++"\
  && make install

# Runner
FROM alpine:3
RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /src/discordchess .
COPY --from=stockfish /usr/local/bin/stockfish /bin/stockfish

ENTRYPOINT ["./discordchess"]
