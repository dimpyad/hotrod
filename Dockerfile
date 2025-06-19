FROM golang:1.22-alpine


RUN mkdir -p /app && echo "Created /app directory ✅"

COPY ./dist/linux/amd64/bin/hotrod /app/hotrod
RUN echo "✅ Contents of /app:" && ls -lh /app && file /app/hotrod
RUN chmod +x /app/hotrod

ENTRYPOINT ["/app/hotrod"]


