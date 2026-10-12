# One image, two roles: it serves the fixture application (a static binary and nothing else), and it runs
# the suite against any URL by attaching to a browser that is already running.
#
# The browser is deliberately not in here. Chromium in an image drags in a libc, fonts, a sandbox and a
# hundred megabytes of surface area; a distroless image that attaches to one over the DevTools protocol is
# smaller, has no shell to break into, and is how the chart runs it - with the browser as a sidecar.
FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orchestrator ./cmd/orchestrator

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/orchestrator /usr/local/bin/orchestrator
# The compose file mounts the scenarios and the report into /app, and a working directory that
# does not exist is a container that fails before it does anything.
WORKDIR /app
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/orchestrator"]
CMD ["serve", "--addr", "0.0.0.0:8080"]
