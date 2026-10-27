# build stage
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY internal/ internal/
COPY cmd/ cmd/
RUN go build -trimpath -ldflags="-s -w" -o /canvas-server ./cmd/canvas-server

# run stage
FROM alpine:3.21
RUN adduser -D -u 1000 canvas
USER canvas
COPY --from=build /canvas-server /canvas-server
EXPOSE 8080
ENTRYPOINT ["/canvas-server"]
CMD ["-addr", ":8080", "-base-url", "http://localhost:8080"]
