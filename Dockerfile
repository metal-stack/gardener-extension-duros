FROM golang:1.23 AS builder

WORKDIR /go/src/github.com/metal-stack/gardener-extension-duros
COPY . .
RUN make install \
 && strip /go/bin/gardener-extension-duros

FROM alpine:3.21
WORKDIR /
COPY charts /charts
COPY --from=builder /go/bin/gardener-extension-duros /gardener-extension-duros
CMD ["/gardener-extension-duros"]
