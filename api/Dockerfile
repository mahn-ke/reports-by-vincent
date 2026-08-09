FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o api .

FROM alpine:3.21
RUN adduser -D -u 1000 app
USER app
WORKDIR /app
COPY --from=build /app/api .
EXPOSE 8080
CMD ["./api"]
