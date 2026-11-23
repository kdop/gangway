# Build from the top folder of the repo: the controller needs shared/.
#   docker build -f controller/Dockerfile .
FROM golang:1.27.4 AS build
WORKDIR /src
COPY shared shared
COPY controller controller
WORKDIR /src/controller
RUN CGO_ENABLED=0 go build -trimpath -o /gangway-controller .

# This base holds the certificates Tailscale's servers are checked with,
# and no shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gangway-controller /gangway-controller
# The key file and the Tailscale identity live under /data. Mount a folder
# there, or the controller is a new device after every restart.
ENV HOME=/data XDG_CONFIG_HOME=/data/config XDG_STATE_HOME=/data/state
ENTRYPOINT ["/gangway-controller"]
