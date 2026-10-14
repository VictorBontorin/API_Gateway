FROM golang:1.25 AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12
COPY --from=build /gateway /gateway
COPY routes.json /routes.json
ENTRYPOINT ["/gateway"]