FROM golang:1.25-alpine AS builder

WORKDIR /src
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /bin/bot-dialog-generator-fakes ./tests/fakes

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /bin/bot-dialog-generator-fakes .

EXPOSE 8090
ENV PORT=8090
ENTRYPOINT ["/app/bot-dialog-generator-fakes"]
