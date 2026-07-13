FROM golang:alpine AS builder
WORKDIR /app
COPY . .
RUN go env -w GOPROXY=https://mirrors.aliyun.com/goproxy/,direct
RUN go build main.go

FROM alpine AS dir
WORKDIR /app
COPY --from=builder /app/h5 ./h5
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/main ./main

FROM scratch
WORKDIR /app
COPY --from=dir /app /app
ENV GIN_MODE release
EXPOSE 8080
ENTRYPOINT ["/app/main"]