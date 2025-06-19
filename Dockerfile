FROM golang:1.22-alpine


RUN mkdir -p /app && echo "Created /app directory ✅"

COPY dist/linux/amd64/bin/hotrod /app/hotrod
RUN echo "Copied binary to /app ✅" && ls -lh /app
RUN chmod +x /app/hotrod

ENTRYPOINT ["/app/hotrod"]


