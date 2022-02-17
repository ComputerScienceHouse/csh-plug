FROM golang:1.16 as builder

WORKDIR /build
COPY * . 
RUN CGO_ENABLED=0 GOOS=linux go build -a -o csh-plug .

FROM alpine:latest

COPY --from=builder /build/csh-plug .
COPY ./templates ./templates
ENTRYPOINT ["./csh-plug"]
