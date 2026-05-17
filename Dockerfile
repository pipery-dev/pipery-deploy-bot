FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/pipery-deploy-bot ./cmd/pipery-deploy-bot

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pipery-deploy-bot /pipery-deploy-bot
COPY migrations /migrations
EXPOSE 8080
ENTRYPOINT ["/pipery-deploy-bot"]
