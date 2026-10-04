# The API server. The website is deployed separately (see docs/DEPLOY.md).

FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# Distroless: no shell, no package manager, runs as a non-root user. The
# agent has no tool that executes anything, and there would be nothing here
# for it to execute.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
