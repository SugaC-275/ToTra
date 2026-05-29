FROM golang:1.25-alpine AS build
WORKDIR /src
COPY main.go .
RUN go mod init mock-upstream && \
    go build -o /mock-upstream .

FROM alpine:3.20
COPY --from=build /mock-upstream /mock-upstream
ENTRYPOINT ["/mock-upstream"]
CMD ["--latency", "100", "--port", "8080"]
