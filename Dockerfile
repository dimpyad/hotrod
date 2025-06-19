FROM golang:1.22-alpine

COPY ./dist/linux/amd64/bin/hotrod /app/hotrod

ENTRYPOINT ["/app/hotrod"]



