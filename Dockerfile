FROM golang:1.24-alpine AS build

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o server ./cmd/server

FROM alpine:3.19

RUN apk add --no-cache sqlite-libs

WORKDIR /app

COPY --from=build /app/server ./server
COPY --from=build /app/web ./web

EXPOSE 8080

CMD ["./server"]
