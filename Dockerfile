FROM golang:alpine as build
RUN apk --no-cache --update upgrade && apk --no-cache add git build-base

ADD . /go/src/github.com/mback2k/go-getmail
WORKDIR /go/src/github.com/mback2k/go-getmail
ENV GO111MODULE on

RUN go get
RUN go build -ldflags="-s -w"
RUN chmod +x go-getmail

FROM mback2k/alpine:latest
RUN apk --no-cache --update upgrade && apk --no-cache add ca-certificates

COPY --from=build /go/src/github.com/mback2k/go-getmail/go-getmail /usr/local/bin/go-getmail

RUN addgroup -g 993 -S serve
RUN adduser -u 993 -h /data -S -D -G serve serve

WORKDIR /data
USER serve

CMD [ "/usr/local/bin/go-getmail" ]