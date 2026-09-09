FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g adapters/rest/main.go -o docs
RUN CGO_ENABLED=0 go build -o /blindpulse-api ./adapters/rest

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /blindpulse-api /blindpulse-api
EXPOSE 8080
ENTRYPOINT ["/blindpulse-api"]
