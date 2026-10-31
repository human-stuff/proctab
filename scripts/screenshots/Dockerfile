# Demo environment used only to take README screenshots
# (no personal processes, paths or addresses end up in the images).
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=v0.1.1" -o /proctab ./cmd/proctab

FROM alpine:3.20
RUN apk add --no-cache nginx redis dropbear dnsmasq memcached python3 \
 && mkdir -p /etc/dropbear /run/nginx \
 && adduser -D -h /srv/app app
COPY --from=build /proctab /usr/local/bin/proctab
COPY scripts/screenshots/start.sh /start.sh
COPY scripts/screenshots/worker.py /srv/app/worker.py
ENV TERM=xterm-256color
ENTRYPOINT ["sh", "/start.sh"]
