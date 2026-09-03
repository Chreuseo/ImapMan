FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /imapman ./cmd/imapman

FROM alpine:3.21
RUN addgroup -S imapman && adduser -S imapman -G imapman
COPY --from=build /imapman /usr/local/bin/imapman
USER imapman
EXPOSE 8080
ENTRYPOINT ["imapman"]
