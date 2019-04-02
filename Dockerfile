FROM golang:1.10-alpine as builder
COPY . /go/src/github.com/tibold/docker-volume-sharedfs
WORKDIR /go/src/github.com/tibold/docker-volume-sharedfs
RUN set -ex \
    && apk add --no-cache --virtual .build-deps \
    gcc libc-dev \
    && go install --ldflags '-extldflags "-static"' \
    && apk del .build-deps
CMD ["/go/bin/docker-volume-sharedfs"]

FROM alpine
RUN mkdir -p /run/docker/plugins /volumes
COPY --from=builder /go/bin/docker-volume-sharedfs .
CMD ["docker-volume-sharedfs"]