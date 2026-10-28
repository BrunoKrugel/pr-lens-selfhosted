# web stage: bundle the canvas viewer (renderer + crypto shim)
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build && mkdir /out && cp -r /internal/webui/dist /out/dist

# build stage
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY internal/ internal/
COPY cmd/ cmd/
COPY --from=web /out/dist/ internal/webui/dist/
RUN go build -trimpath -ldflags="-s -w" -o /canvas-server ./cmd/canvas-server

# run stage
FROM alpine:3.21
RUN adduser -D -u 1000 canvas
USER canvas
COPY --from=build /canvas-server /canvas-server
EXPOSE 8080
ENTRYPOINT ["/canvas-server"]
CMD ["-addr", ":8080", "-base-url", "http://localhost:8080"]
