FROM golang:1.23 AS builder

WORKDIR /go/src/github.com/metal-stack/gardener-extension-duros-provider
COPY . .
RUN make install \
 && strip /go/bin/gardener-extension-duros-provider

FROM alpine:3.21
WORKDIR /
COPY charts /charts
COPY --from=builder /go/bin/gardener-extension-duros-provider /gardener-extension-duros-provider
CMD ["/gardener-extension-duros-provider"]
