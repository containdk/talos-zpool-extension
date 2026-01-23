FROM golang:1.21-alpine AS builder

WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 go build -o create-zpool main.go

FROM scratch

COPY manifest.yaml /manifest.yaml
COPY zpool-creator.yaml /rootfs/usr/local/etc/containers/zpool-creator.yaml
COPY --from=builder /src/create-zpool /rootfs/usr/local/lib/containers/zpool-creator/create-zpool
