# Stage 1: compile the Go service. The Home Assistant builder runs this whole
# Dockerfile under the target architecture (via qemu), so a native build of the
# matching golang image produces a correct binary for each arch.
FROM golang:1.24-alpine AS build

WORKDIR /src
# Cache dependencies first.
COPY src/go.mod src/go.sum ./
RUN go mod download

COPY src/ ./
ARG BUILD_VERSION=dev
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X main.version=${BUILD_VERSION}" \
    -o /eebus-bridge .

# Stage 2: Home Assistant base image (alpine + s6-overlay + bashio).
ARG BUILD_FROM
FROM ${BUILD_FROM}

# s6 v3 in the HA base images still honours legacy /etc/services.d services.
COPY rootfs /
COPY --from=build /eebus-bridge /usr/bin/eebus-bridge
RUN chmod +x /usr/bin/eebus-bridge /etc/services.d/eebus/run /etc/services.d/eebus/finish

# Image metadata
ARG BUILD_ARCH
ARG BUILD_DATE
ARG BUILD_REF
ARG BUILD_VERSION
LABEL \
  io.hass.name="EEBUS Bridge" \
  io.hass.description="EEBUS SHIP/SPINE bridge exposing LPC/LPP/MPC/MOI/MOB to Home Assistant" \
  io.hass.arch="${BUILD_ARCH}" \
  io.hass.type="addon" \
  io.hass.version="${BUILD_VERSION}"
