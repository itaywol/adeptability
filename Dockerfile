FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY adept /usr/local/bin/adept
ENTRYPOINT ["/usr/local/bin/adept"]
